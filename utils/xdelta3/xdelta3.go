package xdelta3

import (
	"bytes"
)

func Decode(data, delta []byte) (out []byte, err error) {
	// first try
	if out, err = DecodeVCDiff(data, delta); err == nil {
		return out, nil
	}

	delta, err = transcodeToVCDiff(delta)
	if err != nil {
		return nil, err
	}
	return DecodeVCDiff(data, delta)
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
