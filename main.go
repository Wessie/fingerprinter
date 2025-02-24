package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"
	"unsafe"

	"github.com/R-a-dio/valkyrie/streamer/audio"
	"github.com/Wessie/fingerprinter/generator"
	"github.com/Wessie/fingerprinter/storage"
)

// PCMLength calculates the expected duration of a file
// that contains PCM audio data in the AudioFormat given
func PCMLength(af audio.Format, size int) time.Duration {
	return time.Duration(size) * time.Second /
		time.Duration(8*44100)
}

func main() {
	ctx := context.Background()

	db, err := storage.NewSQLiteClient("test.db")
	if err != nil {
		log.Println(err)
		return
	}
	defer db.Close()

	for id, filename := range os.Args[1:] {
		fmt.Println(filename)
		err := FingerprintFile(ctx, db, id, filename)
		if err != nil {
			log.Println(err)
			return
		}
	}
}

func FingerprintFile(ctx context.Context, db storage.Storage, id int, filename string) error {
	format := audio.Format{
		Type:     audio.TypeFloat,
		Size:     audio.Size64Bit,
		Endian:   audio.LittleEndian,
		Channels: 1,
	}
	var dur time.Duration

	f, err := audio.DecodeFileAdvanced(ctx, filename, format)
	if err != nil {
		return err
	}
	defer f.Close()

	mapped, err := f.Map()
	if err != nil {
		return err
	}

	dur = PCMLength(format, len(mapped))
	fmt.Println(dur)

	samples := unsafe.Slice((*float64)(unsafe.Pointer(unsafe.SliceData(mapped))), len(mapped)/8)

	spectro, err := generator.Spectrogram(samples, 44100)
	if err != nil {
		return err
	}

	peaks := generator.ExtractPeaks(spectro, dur)
	fingerprints := generator.FingerprintIter(peaks, uint32(id))

	err = db.StoreFingerprints(fingerprints)
	if err != nil {
		return err
	}
	return nil
}
