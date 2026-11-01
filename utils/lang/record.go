package lang

import (
	"encoding/binary"
	"errors"
)

type Record struct {
	Index int
	Cells []Cell
	Label string
}

func (r *Record) IsJapanese() bool {
	return len(r.Cells) == 1
}

func (r *Record) SetCellString(i int, s string) error {
	if len(r.Cells) <= i {
		return errors.New("invalid cell index")
	}

	if r.IsJapanese() {
		if err := r.Cells[i].ToEUCJP(s); err != nil {
			return err
		}
	} else {
		if err := r.Cells[i].ToUTF8(s); err != nil {
			return err
		}
	}
	return nil
}

func (r *Record) SetCellsString(s []string) error {
	if len(s) != len(r.Cells) {
		return errors.New("mismatched cell count")
	}

	if r.IsJapanese() {
		for i, c := range r.Cells {
			if err := c.ToEUCJP(s[i]); err != nil {
				return err
			}
		}
	} else {
		for i, c := range r.Cells {
			if err := c.ToUTF8(s[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Record) CellString(i int) string {
	if len(r.Cells) <= i {
		return ""
	}

	if r.IsJapanese() {
		return r.Cells[i].AsEUCJP()
	} else {
		return r.Cells[i].AsUTF8()
	}
}

func (r *Record) CellsString() []string {
	result := make([]string, 0, len(r.Cells))
	if r.IsJapanese() {
		for _, c := range r.Cells {
			result = append(result, c.AsEUCJP())
		}
	} else {
		for _, c := range r.Cells {
			result = append(result, c.AsUTF8())
		}
	}
	return result
}

func (r *Record) UnmarshalBinary(buf []byte) error {
	cellCount := binary.LittleEndian.Uint16(buf) / 2
	for i := range cellCount {
		offset := i*2 + binary.LittleEndian.Uint16(buf[i*2:])
		var c Cell
		if err := c.UnmarshalBinary(buf[offset:]); err != nil {
			return err
		}
		r.Cells = append(r.Cells, c)
	}
	if len(r.Cells) < 2 {
		return errors.New("too few cells")
	}

	r.Label = r.Cells[len(r.Cells)-1].AsUTF8()
	r.Cells = r.Cells[:len(r.Cells)-1]

	return nil
}

func (r *Record) MarshalBinary() (buf []byte, err error) {

	cells := make([]Cell, 0, len(r.Cells)+1)
	cells = append(cells, r.Cells...)
	cells = append(cells, Cell(r.Label))

	buf = make([]byte, 2*len(cells))

	offset := 0
	for i, c := range cells {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(2*len(cells)+offset-2*i))
		cellBuf, err := c.MarshalBinary()
		if err != nil {
			return nil, err
		}
		offset += len(cellBuf)
		buf = append(buf, cellBuf...)
	}

	return buf, nil
}
