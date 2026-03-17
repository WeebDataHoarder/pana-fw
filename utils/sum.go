package utils

import (
	"encoding/binary"
	"math/bits"
)

func ChecksumAFFS(data []byte) (result uint32) {
	for i := 0; i < len(data); i += 4 {
		result += binary.BigEndian.Uint32(data[i:])
	}
	return -result
}

func Checksum32(data []byte) uint32 {
	var sum uint32
	for i := range data {
		sum += uint32(data[i])
	}
	return bits.RotateLeft32(sum, 16)
}
