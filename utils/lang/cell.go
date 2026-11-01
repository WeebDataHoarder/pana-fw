package lang

import (
	"bytes"
	"io"
	"slices"
)

type Cell []byte

func (c *Cell) UnmarshalBinary(buf []byte) error {
	before, _, ok := bytes.Cut(buf, []byte{0})
	if !ok {
		return io.ErrUnexpectedEOF
	}
	*c = before
	return nil
}

func (c *Cell) MarshalBinary() (buf []byte, err error) {
	data, _, _ := bytes.Cut(*c, []byte{0})
	buf = slices.Clone(data)
	buf = append(buf, byte(0))

	// pad to even
	if len(buf)%2 != 0 {
		buf = append(buf, byte(0))
	}
	return buf, nil
}

func (c *Cell) AsEUCJP() string {
	s, err := ToUTF8(*c)
	if err != nil {
		panic(err)
	}
	return s
}

func (c *Cell) AsUTF8() string {
	return string(*c)
}

func (c *Cell) ToUTF8(s string) error {
	*c = Cell(s)
	return nil
}

func (c *Cell) ToEUCJP(s string) error {
	buf, err := ToEUCJPms(s)
	if err != nil {
		return err
	}
	*c = buf
	return nil
}
