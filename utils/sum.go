package utils

import "encoding/binary"

func ChecksumAFFS(data []byte) (result uint32) {
	for i := 0; i < len(data); i += 4 {
		result += binary.BigEndian.Uint32(data[i:])
	}
	return -result
}

func Checksum32(data []byte) uint32 {
	var sum int32
	for i := range data {
		sum += int32(data[i])
	}
	return uint32(-sum)
}
