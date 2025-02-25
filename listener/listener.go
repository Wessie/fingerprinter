package listener

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wessie/fingerprinter/generator"
	"github.com/drgolem/go-mpg123/mpg123"
	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
	"github.com/rs/zerolog"
)

const maxMetadataLength = 255 * 16

func ListenAndMatch(ctx context.Context, matcher *generator.Matcher) error {

	decoder, err := mpg123.NewDecoder("")
	if err != nil {
		return err
	}
	defer decoder.Delete()

	// force output format
	decoder.FormatNone()
	decoder.Format(44100, 1, mpg123.ENC_SIGNED_16)

	// open the decoder to Feed calls
	if err = decoder.OpenFeed(); err != nil {
		return err
	}

	// start the listener
	ln, err := Listen(ctx, os.Getenv("STREAM_ENDPOINT"), func(ctx context.Context, data []byte) error {
		return decoder.Feed(data)
	})
	if err != nil {
		return err
	}
	defer ln.Close()

	// playback audio for testing if it's badly borked
	feed := make(chan []byte, 12)
	log.Println("new client")
	go func() {
		c, err := pulse.NewClient()
		if err != nil {
			log.Println(err)
			return
		}
		defer c.Close()

		log.Println(decoder.GetFormat())
		log.Println("playback")

		var data []byte
		stream, err := c.NewPlayback(NewFormatReader(proto.FormatInt16LE, func(out []byte) (int, error) {
			if len(data) == 0 {
				select {
				case data = <-feed:
				case <-ctx.Done():
					return 0, ctx.Err()
				}
			}

			n := copy(out, data)
			data = data[n:]
			return n, nil
		}), pulse.PlaybackMono, pulse.PlaybackSampleRate(44100))
		if err != nil {
			log.Println(err)
			return
		}

		log.Println("starting")
		stream.Start()
		<-ctx.Done()
		stream.Drain()
	}()
	// end playback

	var current string
	// setup result matching at end of songs
	var resultMu sync.Mutex
	var result = map[string]float64{}
	var previousTimestamps = map[uint32]uint32{}
	go func() {
		for {
			select {
			case <-ctx.Done():
			case metadata := <-ln.metadataCh:
				resultMu.Lock()
				current = metadata
				var highest float64
				var highestMetadata string
				for metadata, score := range result {
					if highest < score {
						highest = score
						highestMetadata = metadata
					}
				}
				clear(result)
				resultMu.Unlock()

				log.Println("song is probably:")
				log.Println("\t", highest, highestMetadata)
			}
		}
	}()

	const amountOfSeconds = 20
	// 10 seconds of audio
	buf := make([]byte, 44100*2*amountOfSeconds)

	readFull := func(ctx context.Context, out []byte) error {
		var n int
		for n < len(out) && ctx.Err() == nil {
			var nn int
			nn, err := decoder.Read(out[n:])
			if err != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
				}
			}
			n += nn
		}
		return nil
	}

	var half = len(buf) / 2

	// initialize the first half of the buffer
	err = readFull(ctx, buf[:half])
	if err != nil {
		return err
	}

	for ctx.Err() == nil {
		// we want to fill the whole buffer and then slap it to the fingerprinter
		err = readFull(ctx, buf[half:])
		if err != nil {
			return err
		}

		first := sha1.Sum(buf[:half])
		second := sha1.Sum(buf[half:])

		fmt.Println(hex.EncodeToString(first[:]), hex.EncodeToString(second[:]))

		feed <- bytes.Clone(buf[half:])

		go func(fbuf []float64) {
			matches, took, err := matcher.Find(fbuf, time.Second*amountOfSeconds, 44100)
			fmt.Println(took)
			if err != nil {
				log.Println(err)
				return
			}

			resultMu.Lock()
			for i, match := range matches {
				_ = i

				delta := int64(match.Timestamp) - int64(previousTimestamps[match.SongID])
				if delta > (amountOfSeconds/2-2)*1000 && delta < (amountOfSeconds/2+2)*1000 {
					match.Score *= 2
				}

				if i < 100 {
					fmt.Println(i, match.Score, match.Metadata)
				}
				if strings.HasPrefix(match.Metadata, current) {
					fmt.Println(i, match.Score, match.Timestamp, delta, match.Metadata)
				}
				/*if i < 25 {
					fmt.Println(match.Score, "|", delta, match.Timestamp, "|", match.Metadata)
				}*/

				previousTimestamps[match.SongID] = match.Timestamp
				result[match.Metadata] = result[match.Metadata] + match.Score
			}
			resultMu.Unlock()
		}(generator.S16LEToF64LE(buf))

		copy(buf, buf[half:])
	}

	return ctx.Err()
}

func Execute(ctx context.Context) error {
	log.Println("making decoder")
	decoder, err := mpg123.NewDecoder("")
	if err != nil {
		return err
	}
	defer decoder.Delete()

	decoder.FormatNone()
	decoder.Format(44100, 1, mpg123.ENC_SIGNED_16)

	if err = decoder.OpenFeed(); err != nil {
		return err
	}

	ln, err := Listen(ctx, os.Getenv("STREAM_ENDPOINT"), func(ctx context.Context, data []byte) error {
		//fmt.Println("write data:", len(data))
		return decoder.Feed(data)
	})
	if err != nil {
		return err
	}
	defer ln.Close()

	log.Println("new client")
	c, err := pulse.NewClient()
	if err != nil {
		return err
	}
	defer c.Close()

	log.Println(decoder.GetFormat())
	log.Println("playback")
	stream, err := c.NewPlayback(NewFormatReader(proto.FormatInt16LE, func(out []byte) (int, error) {
		//		log.Println("data ask")
		n, err := decoder.Read(out)
		if err != nil && n == 0 {
			time.Sleep(time.Second / 2)
			log.Println("data err:", err)
			return 0, nil
		}

		//log.Println(decoder.GetFormat())
		//log.Println("data written", n)
		return n, nil
	}), pulse.PlaybackMono, pulse.PlaybackSampleRate(44100))
	if err != nil {
		return err
	}

	log.Println("starting")
	stream.Start()
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt)
	<-ch

	log.Println("draining")
	stream.Drain()
	log.Println("closing")
	stream.Close()
	log.Println("leaving")
	log.Println(stream.Error())
	return nil
}

func NewFormatReader(format byte, read func([]byte) (int, error)) FormatReader {
	return FormatReader{
		format: format,
		read:   read,
	}
}

type FormatReader struct {
	format byte
	read   func([]byte) (int, error)
}

func (fr FormatReader) Read(out []byte) (int, error) {
	return fr.read(out)
}

func (fr FormatReader) Format() byte {
	return fr.format
}

// listener listens to an icecast mp3 stream with interleaved song metadata
type listener struct {
	// cancel is called when Close is called and cancels all in-progress reads
	cancel     context.CancelFunc
	done       chan struct{}
	handleData func(ctx context.Context, data []byte) error
	metadataCh chan string
}

func Listen(ctx context.Context, u string, dataFn func(ctx context.Context, data []byte) error) (*listener, error) {
	uri, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("Listen: failed to parse url: %w", err)
	}
	return ListenURL(ctx, uri, dataFn), nil
}
func ListenURL(ctx context.Context, u *url.URL, dataFn func(ctx context.Context, data []byte) error) *listener {
	ln := listener{
		metadataCh: make(chan string),
		done:       make(chan struct{}),
		handleData: dataFn,
	}
	ctx, ln.cancel = context.WithCancel(ctx)
	go func() {
		defer ln.cancel()
		defer close(ln.done)
		ln.run(ctx, u)
	}()
	return &ln
}

// Shutdown signals the listener to stop running, and waits for it to exit
func (ln *listener) Close() error {
	ln.cancel()
	<-ln.done
	return nil
}
func (ln *listener) run(ctx context.Context, u *url.URL) {
	logger := zerolog.Ctx(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn, metasize, err := ln.newConn(ctx, u)
		if err != nil {
			logger.Error().Err(err).Msg("connecting")
			// wait a bit before retrying the connection
			select {
			case <-ctx.Done():
			case <-time.After(time.Second * 2):
			}
			continue
		}
		err = ln.parseResponse(ctx, metasize, conn)
		if err != nil {
			// log the error, and try reconnecting
			logger.Error().Err(err).Msg("connection")
		}
	}
}
func (ln *listener) newConn(ctx context.Context, u *url.URL) (io.ReadCloser, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Host = "r-a-d.io"
	// we don't want to re-use connections for the audio stream
	req.Close = true
	// we want interleaved metadata so we have to ask for it
	req.Header.Add("Icy-MetaData", "1")
	req.Header.Set("User-Agent", "hanyuu/relay")
	// TODO: don't use the default client
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to do request: %w", err)
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("status code is not OK was %d: %s", resp.StatusCode, resp.Status)
	}
	// convert the metadata size we got back from the server
	metasize, err := strconv.Atoi(resp.Header.Get("icy-metaint"))
	if err != nil {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("icy-metaint is not an integer: %w", err)
	}
	return resp.Body, metasize, nil
}
func (ln *listener) parseResponse(ctx context.Context, metasize int, src io.Reader) error {
	r := bufio.NewReader(src)
	logger := zerolog.Ctx(ctx)
	var meta map[string]string
	var buf = make([]byte, metasize)
	if metasize <= maxMetadataLength {
		// we allocate one extra byte to support semicolon insertion in
		// metadata parsing
		buf = make([]byte, maxMetadataLength+1)
	}
	for {
		// we first get actual mp3 data from icecast
		_, err := io.ReadFull(r, buf[:metasize])
		if err != nil {
			return err
		}
		if ln.handleData != nil {
			err = ln.handleData(ctx, buf[:metasize])
			if err != nil {
				logger.Err(err).Msg("failed handling mp3 data")
			}
		}
		// then we get a single byte indicating metadata length
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		// if the length is set to 0 we're not expecting any metadata and can
		// read data again
		if b == 0 {
			continue
		}
		// else metadata length needs to be multiplied by 16 from the wire
		length := int(b * 16)
		_, err = io.ReadFull(r, buf[:length])
		if err != nil {
			return err
		}
		// now parse the metadata
		meta = parseMetadata(buf[:length])
		if len(meta) == 0 {
			// fatal because it most likely means we've lost sync with the data
			// stream and can't find our metadata anymore.
			return errors.New("listener: empty metadata: " + string(buf[:length]))
		}
		song := meta["StreamTitle"]
		if song == "" {
			logger.Info().Msg("empty metadata")
			continue
		}
		go func() {
			select {
			case ln.metadataCh <- song:
			case <-ctx.Done():
			}
		}()
	}
}
func parseMetadata(b []byte) map[string]string {
	var meta = make(map[string]string, 2)
	// trim any padding nul bytes and insert a trailing semicolon if one
	// doesn't exist yet
	for i := len(b) - 1; i > 0; i-- {
		if b[i] == '\x00' {
			continue
		}
		if b[i] == ';' {
			// already have a trailing semicolon
			b = b[:i+1]
			break
		}
		// don't have one, so add one
		b = append(b[:i+1], ';')
		break
	}
	for {
		var key, value string
		b, key = findSequence(b, '=', '\'')
		b, value = findSequence(b, '\'', ';')
		if key == "" {
			break
		}
		// try and do any html escaping, icecast default configuration will send unicode chars
		// as html escaped characters
		value = html.UnescapeString(value)
		// replace any broken utf8, since other layers expect valid utf8 we do it at the edge
		value = strings.ToValidUTF8(value, string(utf8.RuneError))
		meta[key] = value
	}
	return meta
}
func findSequence(seq []byte, a, b byte) ([]byte, string) {
	for i := 1; i < len(seq); i++ {
		if seq[i-1] == a && seq[i] == b {
			return seq[i+1:], string(seq[:i-1])
		}
	}
	return nil, ""
}
