package util

import "unsafe"

func AsFloat64(in []byte) []float64 {
	return unsafe.Slice((*float64)(unsafe.Pointer(unsafe.SliceData(in))), len(in)/8)
}

func AsByte(in []float64) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(in))), len(in)*8)
}
