package generator

import (
	"cmp"
	"fmt"
	"log"
	"math"
	"slices"
	"time"

	"github.com/Wessie/fingerprinter/storage"
)

type Match struct {
	SongID     uint32
	SongTitle  string
	SongArtist string
	YouTubeID  string
	Timestamp  uint32
	Score      float64
}

type Matcher struct {
	db storage.Storage
}

// FindMatches processes the audio samples and finds matches in the database
func (m Matcher) Find(audioSamples []float64, audioDuration time.Duration, sampleRate int) ([]Match, time.Duration, error) {
	startTime := time.Now()

	spectrogram, err := Spectrogram(audioSamples, sampleRate)
	if err != nil {
		return nil, time.Since(startTime), fmt.Errorf("failed to get spectrogram of samples: %v", err)
	}

	peaks := ExtractPeaks(spectrogram, audioDuration)
	fingerprints := Fingerprint(peaks, 0)

	addresses := make([]uint32, 0, len(fingerprints))
	for address := range fingerprints {
		addresses = append(addresses, address)
	}

	matchCouples, err := m.db.GetCouples(addresses)
	if err != nil {
		return nil, time.Since(startTime), err
	}

	matches := map[uint32][][2]uint32{} // songID -> [(sampleTime, dbTime)]
	timestamps := map[uint32][]uint32{}

	for address, couples := range matchCouples {
		for _, couple := range couples {
			matches[couple.SongID] = append(matches[couple.SongID], [2]uint32{fingerprints[address].AnchorTimeMs, couple.AnchorTimeMs})
			timestamps[couple.SongID] = append(timestamps[couple.SongID], couple.AnchorTimeMs)
		}
	}

	scores := analyzeRelativeTiming(matches)

	var matchList []Match
	for songID, points := range scores {
		song, songExists, err := m.db.GetSongByID(songID)
		if !songExists {
			log.Printf("song with ID (%v) doesn't exist", songID)
			continue
		}
		if err != nil {
			log.Printf("failed to get song by ID (%v): %v", songID, err)
			continue
		}

		slices.Sort(timestamps[songID])

		match := Match{songID, song.Title, song.Artist, song.YouTubeID, timestamps[songID][0], points}
		matchList = append(matchList, match)
	}

	slices.SortFunc(matchList, func(i, j Match) int {
		return cmp.Compare(i.Score, j.Score)
	})

	return matchList, time.Since(startTime), nil
}

// AnalyzeRelativeTiming checks for consistent relative timing and returns a score
func analyzeRelativeTiming(matches map[uint32][][2]uint32) map[uint32]float64 {
	scores := make(map[uint32]float64)
	for songID, times := range matches {
		count := 0
		for i := 0; i < len(times); i++ {
			for j := i + 1; j < len(times); j++ {
				sampleDiff := math.Abs(float64(times[i][0] - times[j][0]))
				dbDiff := math.Abs(float64(times[i][1] - times[j][1]))
				if math.Abs(sampleDiff-dbDiff) < 100 { // Allow some tolerance
					count++
				}
			}
		}
		scores[songID] = float64(count)
	}
	return scores
}
