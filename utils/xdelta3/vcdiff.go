package xdelta3

import "github.com/ably/vcdiff-go"

func DecodeVCDiff(data, delta []byte) ([]byte, error) {
	return vcdiff.Decode(data, delta)
}
