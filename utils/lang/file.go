package lang

import "encoding/binary"

type File []Record

func (f *File) UnmarshalBinary(buf []byte) error {
	recordCount := binary.LittleEndian.Uint32(buf) / 4
	for i := range recordCount {
		offset := i*4 + binary.LittleEndian.Uint32(buf[i*4:])
		var r Record
		if err := r.UnmarshalBinary(buf[offset:]); err != nil {
			return err
		}
		r.Index = int(i)
		*f = append(*f, r)
	}
	return nil
}

func (f *File) MarshalBinary() (buf []byte, err error) {
	buf = make([]byte, 4*len(*f))

	offset := 0
	for i, r := range *f {
		binary.LittleEndian.PutUint32(buf[i*4:], uint32(4*len(*f)+offset-4*i))
		recordBuf, err := r.MarshalBinary()
		if err != nil {
			return nil, err
		}
		offset += len(recordBuf)
		buf = append(buf, recordBuf...)
	}

	return buf, nil
}

func (f *File) RecordById(i int) *Record {
	if len(*f) <= i {
		return nil
	}
	return &(*f)[i]
}

func (f *File) RecordsByLabel(label string) (results []*Record) {
	for i := range *f {
		if (*f)[i].Label == label {
			results = append(results, &(*f)[i])
		}
	}
	return results
}
