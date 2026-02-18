package lzss

import (
	"errors"
)

// Decompress Decompresses a provided buffer with a window offset tweak.
// For normal usages, windowOffset should be set to 0
func Decompress(b []byte, windowOffset int) (output []byte, err error) {

	windowIndex := (WindowInitialIndex + windowOffset) % WindowSize

	output = make([]byte, 0, DataMaxSize)

	window := CreateWindow()

	windowBuffer := make([]byte, len(window))

	var buf []byte
	var flags, length, literal, offsetIndex uint64
	for len(b) > 0 {
		flags >>= 1
		if flags&0x100 == 0 {
			flags = uint64(b[0]) | 0xFF00
			b = b[1:]
		}

		if flags&1 == 0 {
			if len(b) < 2 {
				return output, errors.New("out of bounds output")
			}
			offsetIndex = uint64(b[0]) | (uint64(b[1]&0xF0) << LengthBits)
			length = uint64(b[1] & 0x0F)
			b = b[2:]
			if length += MaxUncoded; DataMaxSize < (length + uint64(len(output))) {
				return output, errors.New("out of bounds output")
			}

			windowIndex, buf = window.GetSet(int(offsetIndex), windowIndex, int(length), windowBuffer)
			output = append(output, buf...)
		} else {
			if len(b) < 1 {
				return output, errors.New("out of bounds output")
			}

			literal = uint64(b[0])
			b = b[1:]
			if (DataMaxSize - 1) < len(output) {
				return output, errors.New("out of bounds output")
			}

			output = append(output, byte(literal))
			windowIndex = window.SetByte(windowIndex, byte(literal))
		}
	}

	return output, nil
}
