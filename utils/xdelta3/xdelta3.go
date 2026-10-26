package xdelta3

import (
	"bytes"
	"errors"

	"github.com/ably/vcdiff-go"
)

func Decode(data, delta []byte) (out []byte, err error) {
	// first try
	if out, err = DecodeVCDiff(data, delta); err != nil {
		if delta, err = transcodeToVCDiff(delta); err != nil {
			return nil, err
		}
		if out, err = DecodeVCDiff(data, delta); err != nil {
			return nil, err
		}
	}

	parsed, err := vcdiff.ParseDelta(delta)
	if err != nil {
		return nil, err
	}
	for _, w := range parsed.Windows {
		if !w.HasChecksum {
			return nil, errors.New("delta has no checksum, cannot be verified")
		}
	}
	return out, nil
}

func transcodeToVCDiff(in []byte) ([]byte, error) {
	t := &transcoder{r: bytes.NewReader(in)}
	var out bytes.Buffer
	if err := t.rewriteHeader(&out); err != nil {
		return nil, err
	}
	for t.r.Len() > 0 {
		if err := t.rewriteWindow(&out); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}
