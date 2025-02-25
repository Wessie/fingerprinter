package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	radio "github.com/R-a-dio/valkyrie"
	"github.com/R-a-dio/valkyrie/streamer/audio"
	"github.com/Wessie/fingerprinter/generator"
	"github.com/Wessie/fingerprinter/storage"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

// PCMLength calculates the expected duration of a file
// that contains PCM audio data in the AudioFormat given
func PCMLength(af audio.Format, size int) time.Duration {
	return time.Duration(size) * time.Second /
		time.Duration(8*44100)
}

func main() {
	ctx := context.Background()
	ctx = zerolog.New(os.Stdout).Level(zerolog.DebugLevel).WithContext(ctx)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	/*err := listener.Execute(ctx)
	if err != nil {
		log.Println(err)
		return
	}

	return*/
	//go listener.Execute(ctx)

	db, err := storage.NewSQLiteClient("test.db")
	if err != nil {
		log.Println(err)
		return
	}
	defer db.Close()

	/*err = listener.ListenAndMatch(ctx, generator.NewMatcher(db))
	if err != nil {
		log.Println(err)
		return
	}
	return*/

	//MatchFile(ctx, db, os.Args[rand.IntN(len(os.Args[1:]))])
	MatchFile(ctx, db, os.Args[1:][50])
	return

	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(8)
	for _, filename := range os.Args[1:] {
		filename := filename
		group.Go(func() error {
			metadata := filepath.Base(filename)
			key := radio.NewSongHash(metadata)

			id, err := db.RegisterSong(key.String(), metadata)
			if err != nil {
				return err
			}
			if id == 0 {
				return nil
			}

			fmt.Println(id, key, filename)

			err = FingerprintFile(ctx, db, id, filename)
			if err != nil {
				return err
			}
			return nil
		})
	}

	if err = group.Wait(); err != nil {
		log.Println(err)
	}
}

func FingerprintFile(ctx context.Context, db storage.Storage, id uint32, filename string) error {
	format := audio.Format{
		Type:     audio.TypeSigned,
		Size:     audio.Size16Bit,
		Endian:   audio.LittleEndian,
		Channels: 1,
	}

	f, err := audio.DecodeFileAdvanced(ctx, filename, format)
	if err != nil {
		return err
	}
	defer f.Close()

	mapped, err := f.Map()
	if err != nil {
		return err
	}
	defer f.Unmap()

	dur := time.Duration(len(mapped)) * time.Second / time.Duration(2*44100)

	samples := generator.S16LEToF64LE(mapped)
	// we're done with the file now
	f.Close()
	f.Unmap()

	spectro, err := generator.Spectrogram(samples, 44100)
	if err != nil {
		return err
	}

	peaks := generator.ExtractPeaks(spectro, dur)
	fp := generator.Fingerprint(peaks, id)
	//fingerprints := generator.FingerprintIter(peaks, uint32(id))

	err = db.StoreFingerprints(fp)
	if err != nil {
		return err
	}
	_ = fp
	return nil
}

func MatchFile(ctx context.Context, db storage.Storage, filename string) error {
	format := audio.Format{
		Type:     audio.TypeSigned,
		Size:     audio.Size16Bit,
		Endian:   audio.LittleEndian,
		Channels: 1,
	}

	f, err := audio.DecodeFileAdvanced(ctx, filename, format)
	if err != nil {
		return err
	}
	defer f.Close()

	mapped, err := f.Map()
	if err != nil {
		return err
	}
	defer f.Unmap()

	mapped = mapped[30*2*44100:]

	dur := time.Duration(len(mapped)) * time.Second / time.Duration(2*44100)

	samples := generator.S16LEToF64LE(mapped)
	// we're done with the file now
	f.Close()
	f.Unmap()

	matches, took, err := generator.NewMatcher(db).Find(samples, dur, 44100)
	if err != nil {
		return err
	}

	fmt.Println("looking for:", filename)
	fmt.Println(took)
	for _, match := range matches {
		fmt.Println(match.Score, match.Metadata)
		break
	}

	return nil
}
