package generator

import (
	"iter"
	"log"

	"github.com/Wessie/fingerprinter/storage"
)

const (
	maxFreqBits    = 9
	maxDeltaBits   = 14
	targetZoneSize = 5
)

// Fingerprint generates fingerprints from a list of peaks and stores them in an array.
// The fingerprints are encoded using a 32-bit integer format and stored in an array.
// Each fingerprint consists of an address and a couple.
// The address is a hash. The couple contains the anchor time and the song ID.
func Fingerprint(peaks []Peak, songID uint32) map[storage.Address][]storage.Couple {
	fingerprints := map[storage.Address][]storage.Couple{}

	var dupes int
	for i, anchor := range peaks {
		for j := i + 1; j < len(peaks) && j <= i+targetZoneSize; j++ {
			target := peaks[j]

			address := createAddress(anchor, target)
			anchorTimeMs := uint32(anchor.Time * 1000)

			if _, ok := fingerprints[address]; ok {
				log.Println(fingerprints[address], anchorTimeMs)
				dupes++
			}
			fingerprints[address] = append(fingerprints[address], storage.Couple{anchorTimeMs, songID})
			//fingerprints[address] = storage.Couple{anchorTimeMs, songID}
		}
	}

	return fingerprints
}

func FingerprintIter(peaks []Peak, songID uint32) iter.Seq2[storage.Address, storage.Couple] {
	return func(yield func(storage.Address, storage.Couple) bool) {
		for i, anchor := range peaks {
			for j := i + 1; j < len(peaks) && j <= i+targetZoneSize; j++ {
				target := peaks[j]

				address := createAddress(anchor, target)
				anchorTimeMs := uint32(anchor.Time * 1000)

				if !yield(address, storage.Couple{
					AnchorTimeMs: anchorTimeMs,
					SongID:       songID,
				}) {
					return
				}
			}
		}
	}
}

// createAddress generates a unique address for a pair of anchor and target points.
// The address is a 32-bit integer where certain bits represent the frequency of
// the anchor and target points, and other bits represent the time difference (delta time)
// between them. This function combines these components into a single address (a hash).
func createAddress(anchor, target Peak) storage.Address {
	anchorFreq := int32(real(anchor.Freq))
	targetFreq := int32(real(target.Freq))
	deltaMs := storage.Address((target.Time - anchor.Time) * 1000)

	// Combine the frequency of the anchor, target, and delta time into a 32-bit address
	address := storage.Address(anchorFreq<<23) | storage.Address(targetFreq<<14) | deltaMs

	return address
}
