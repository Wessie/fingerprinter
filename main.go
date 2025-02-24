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

func main() {
	ctx := context.Background()
	var filename string

	filename = os.Args[1]

	format := audio.Format{
		Type:     audio.TypeFloat,
		Size:     audio.Size64Bit,
		Endian:   audio.LittleEndian,
		Channels: 1,
	}
	var dur time.Duration

	f, err := audio.DecodeFileAdvanced(ctx, filename, format)
	if err != nil {
		log.Println(err)
		return
	}
	defer f.Close()

	mapped, err := f.Map()
	if err != nil {
		log.Println(err)
		return
	}

	samples := unsafe.Slice((*float64)(unsafe.Pointer(unsafe.SliceData(mapped))), len(mapped)/8)

	spectro, err := generator.Spectrogram(samples, 44100)
	if err != nil {
		log.Println(err)
		return
	}

	var id uint32 = 500
	peaks := generator.ExtractPeaks(spectro, dur)
	fingerprints := generator.Fingerprint(peaks, id)

	_ = fingerprints

	db, err := storage.NewSQLiteClient("test.db")
	if err != nil {
		log.Println(err)
		return
	}
	defer db.Close()

	err = db.StoreFingerprints(fingerprints)
	if err != nil {
		log.Println(err)
		return
	}

	fmt.Println(len(fingerprints))
}
