package generator

import (
	"encoding/binary"
)

// S16LEToF64 converts a slice of bytes from a s16le format to f64le format
func S16LEToF64LE(input []byte) []float64 {
	numSamples := len(input) / 2
	output := make([]float64, numSamples)

	for i := 0; i < len(input); i += 2 {
		// Interpret bytes as a 16-bit signed integer (little-endian)
		sample := int16(binary.LittleEndian.Uint16(input[i : i+2]))

		// Scale the sample to the range [-1, 1]
		output[i/2] = float64(sample) / 32768.0
	}

	return output
}
