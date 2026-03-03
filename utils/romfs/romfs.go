package romfs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path"
	"slices"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
)

const MAGIC = "-rom1fs-"

type RomFS struct {
	Magic       [8]byte
	Size        uint32
	SumFirst512 uint32
	VolumeName  []byte

	Files []File
}

func sum(data []byte) (result uint32) {
	for i := 0; i < len(data); i += 4 {
		result += binary.BigEndian.Uint32(data[i:])
	}
	return -result
}

func NewRomFS(buf []byte) (*RomFS, error) {
	var fs RomFS
	err := fs.Decode(buf)
	if err != nil {
		return nil, err
	}
	return &fs, nil
}

func (fs *RomFS) Decode(buf []byte) (err error) {
	if len(buf) < 1024 {
		return fmt.Errorf("romfs: too small")
	}

	copy(fs.Magic[:], buf)
	if !bytes.Equal(fs.Magic[:], []byte(MAGIC)) {
		return fmt.Errorf("romfs: magic mismatch")
	}
	fs.Size = binary.BigEndian.Uint32(buf[8:])
	fs.SumFirst512 = binary.BigEndian.Uint32(buf[12:])

	if i := bytes.IndexByte(buf[16:], 0); i == -1 {
		return fmt.Errorf("romfs: invalid volume name")
	} else {
		fs.VolumeName = slices.Clone(buf[16 : 16+utils.Align(i+1, 16)])
	}

	sumBuf := slices.Clone(buf[:512])
	// set sum to 0
	binary.BigEndian.PutUint32(sumBuf[12:], 0)

	if sum(sumBuf) != fs.SumFirst512 {
		return fmt.Errorf("romfs: invalid sum")
	}

	offset := 16 + len(fs.VolumeName)

	var currentPath = "/"

	for uint32(offset) < fs.Size {
		var f File
		n, err := f.Decode(buf[offset:])
		if err != nil {
			return err
		}
		offset += n
		f.Name = path.Join(currentPath, utils.CString(f.Header.Name))
		fs.Files = append(fs.Files, f)
		if f.Header.NextFileHeader == 0 {
			// no more files?
			currentPath = path.Dir(currentPath)
		}
		if f.IsDir() {
			currentPath = path.Join(currentPath, utils.CString(f.Header.Name))
		}
	}
	return nil
}

type FileHeader struct {
	NextFileHeader uint32
	Mode           uint32
	SpecInfo       uint32
	Size           uint32
	Sum            uint32
	Name           []byte
}

func (f *File) IsHardLink() bool {
	return f.Header.Mode&7 == 0
}

func (f *File) IsDir() bool {
	return f.Header.Mode&7 == 1
}

func (f *File) IsFile() bool {
	return f.Header.Mode&7 == 2
}

func (f *File) IsSoftLink() bool {
	return f.Header.Mode&7 == 3
}

func (f *File) IsBlockDevice() bool {
	return f.Header.Mode&7 == 4
}

func (f *File) IsCharDevice() bool {
	return f.Header.Mode&7 == 5
}

func (f *File) IsSocket() bool {
	return f.Header.Mode&7 == 6
}

func (f *File) IsFIFO() bool {
	return f.Header.Mode&7 == 7
}

func (f *File) FileMode() int {
	if (f.Header.Mode>>3)&1 > 0 {
		return 0755
	} else {
		return 0644
	}
}

func (f *File) Decode(buf []byte) (n int, err error) {
	hdrBuf := buf

	NextFileHeader := binary.BigEndian.Uint32(buf)
	buf = buf[4:]
	f.Header.NextFileHeader = (NextFileHeader >> 4) << 4
	f.Header.Mode = NextFileHeader & 0b1111
	f.Header.SpecInfo = binary.BigEndian.Uint32(buf)
	buf = buf[4:]
	f.Header.Size = binary.BigEndian.Uint32(buf)
	buf = buf[4:]
	f.Header.Sum = binary.BigEndian.Uint32(buf)
	buf = buf[4:]

	if i := bytes.IndexByte(buf, 0); i == -1 {
		return len(hdrBuf) - len(buf), fmt.Errorf("invalid file name")
	} else {
		f.Header.Name = slices.Clone(buf[:utils.Align(i+1, 16)])
		buf = buf[len(f.Header.Name):]
	}

	sumBuf := slices.Clone(hdrBuf[:len(hdrBuf)-len(buf)])
	binary.BigEndian.PutUint32(sumBuf[12:], 0)

	if sum(sumBuf) != f.Header.Sum {
		return len(hdrBuf) - len(buf), fmt.Errorf("invalid sum")
	}

	if f.Header.Size > 0 {
		f.Data = slices.Clone(buf[:f.Header.Size])
		buf = buf[utils.Align(f.Header.Size, 16):]
	}

	return len(hdrBuf) - len(buf), nil
}

type File struct {
	Header FileHeader
	Name   string
	Data   []byte
}
