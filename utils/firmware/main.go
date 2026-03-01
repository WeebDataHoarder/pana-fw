package firmware

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/adler32"
	"io"
	"time"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/crypto"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/lzss"
)

type CompressionType uint32

const (
	CompressionTypeNone = CompressionType(0)
	CompressionTypeGZIP = CompressionType(1)
	CompressionTypeLZSS = CompressionType(2)
)

type MainListHeader struct {
	Checksum uint32
	// Unknown1 seems to be always 1?
	Unknown1 uint32
	ListSize uint32
	// DecompressedPartsSize The maximum size of each decompressed part. Use MainPartTargetSize
	DecompressedPartsSize uint32
	CompressionType       CompressionType
}

type MainListEntry struct {
	Size     uint32
	Checksum uint32
}

type MainList struct {
	Header   MainListHeader
	Entries  []MainListEntry
	Segments []CompressedFile
	Data     []byte
}

func NewMainListGZIP(data []byte, key [32]byte) (*MainList, error) {
	var out MainList
	out.Header.Unknown1 = 1
	out.Header.ListSize = 20
	out.Header.DecompressedPartsSize = MainPartTargetSize
	out.Header.CompressionType = CompressionTypeGZIP
	out.Data = data

	for offset := 0; offset < len(data); offset += MainPartTargetSize {
		err := func() error {

			part := data[offset:min(offset+MainPartTargetSize, len(data))]

			const checksumSkip = 16

			var cksumBuf []byte

			for i := 0; i < len(part); i += checksumSkip {
				cksumBuf = append(cksumBuf, part[i])
			}

			wBuf := bytes.NewBuffer(nil)
			w, err := gzip.NewWriterLevel(wBuf, gzip.BestCompression)
			if err != nil {
				return err
			}
			defer w.Close()
			w.Header.ModTime = time.Now().UTC()
			w.Header.OS = 3
			w.Header.Name = fmt.Sprintf("bank.rom.in%d.s", offset/MainPartTargetSize)

			if _, err = w.Write(part); err != nil {
				return err
			}
			if err = w.Flush(); err != nil {
				return err
			}
			if err = w.Close(); err != nil {
				return err
			}

			compressed := wBuf.Bytes()

			padded := len(compressed)
			if padded%8 != 0 {
				padded += 8 - padded%8
			}

			segment := CompressedFile{
				Header: CompressedFileHeader{
					Magic:              [8]byte([]byte(CompressedFileHeaderMagic)),
					Unknown:            [4]byte([]byte("DRV ")),
					CompressedFlag:     1,
					CompressionType:    uint16(CompressionTypeGZIP),
					DestinationSize:    uint32(len(part)),
					DestinationAddress: 0,
					SourceSize:         uint32(len(compressed)),
					//TODO???
					SourceAddress: 0,
					FooterOffset:  64 + uint32(padded),
					Unknown2:      [4]byte{},
					Checksum:      adler32.Checksum(cksumBuf),
					ChecksumSkip:  checksumSkip,
					Unused:        [16]byte{},
				},
				Data:         compressed,
				Decompressed: part,
				Child:        nil,
			}

			outBuf, err := segment.ToBuffer()
			if err != nil {
				return err
			}

			if len(outBuf) < MainPartEncryptedSize {
				return errors.New("encrypted block too small")
			} else {
				//yep, this can overlap
				crypto.CustomEncrypt(outBuf[:MainPartEncryptedSize], key)
				crypto.CustomEncrypt(outBuf[len(outBuf)-MainPartEncryptedSize:], key)
			}

			out.Entries = append(out.Entries, MainListEntry{
				Size:     uint32(len(outBuf)),
				Checksum: adler32.Checksum(outBuf),
			})
			out.Segments = append(out.Segments, segment)
			out.Header.ListSize += 8

			return nil
		}()
		if err != nil {
			return nil, err
		}
	}

	return &out, nil
}

const MainPartEncryptedSize = 0x1400
const MainPartTargetSize = 16 * 1024 * 1024

func (l *MainList) ToBuffer(key [32]byte) (buf []byte, err error) {
	hdrBuf := make([]byte, l.Header.ListSize)

	if _, err = binary.Encode(hdrBuf[:20], binary.LittleEndian, l.Header); err != nil {
		return nil, err
	}

	for i, e := range l.Entries {
		if _, err = binary.Encode(hdrBuf[20+i*8:], binary.LittleEndian, e); err != nil {
			return nil, err
		}
	}

	binary.LittleEndian.PutUint32(hdrBuf, adler32.Checksum(hdrBuf[4:]))
	buf = append(buf, hdrBuf...)

	for _, s := range l.Segments {
		part, err := s.ToBuffer()
		if err != nil {
			return nil, err
		}

		if len(part) < MainPartEncryptedSize {
			return nil, errors.New("encrypted block too small")
		} else {
			//yep, this can overlap
			crypto.CustomEncrypt(part[:MainPartEncryptedSize], key)
			crypto.CustomEncrypt(part[len(part)-MainPartEncryptedSize:], key)
		}
		buf = append(buf, part...)
	}

	return buf, nil
}

func (l *MainList) FromReader(r io.ReadSeeker, key [32]byte) (err error) {
	if err = binary.Read(r, binary.LittleEndian, &l.Header); err != nil {
		return err
	}

	if l.Header.ListSize < 20 || (l.Header.ListSize-20)%8 != 0 {
		return errors.New("invalid list size")
	}

	hdrBuf := make([]byte, l.Header.ListSize)
	if _, err = binary.Encode(hdrBuf[:20], binary.LittleEndian, l.Header); err != nil {
		return err
	}

	if _, err = io.ReadFull(r, hdrBuf[20:]); err != nil {
		return err
	}

	if adler32.Checksum(hdrBuf[4:]) != l.Header.Checksum {
		return ErrInvalidHeaderChecksum
	}

	count := (l.Header.ListSize - 20) / 8
	hdrBuf = hdrBuf[20:]

	for range count {
		var e MainListEntry
		if _, err = binary.Decode(hdrBuf, binary.LittleEndian, &e); err != nil {
			return err
		}
		l.Entries = append(l.Entries, e)

		buf := make([]byte, e.Size)
		if _, err = io.ReadFull(r, buf); err != nil {
			return err
		}
		if adler32.Checksum(buf) != e.Checksum {
			return ErrInvalidDataChecksum
		}

		if len(buf) < MainPartEncryptedSize {
			return errors.New("encrypted block too small")
		} else {
			//yep, this can overlap
			crypto.CustomDecrypt(buf[:MainPartEncryptedSize], key)
			crypto.CustomDecrypt(buf[len(buf)-MainPartEncryptedSize:], key)
		}

		var f CompressedFile
		if err = f.FromBuffer(buf); err != nil {
			return fmt.Errorf("could not decompress file: %w", err)
		}

		l.Segments = append(l.Segments, f)
		l.Data = append(l.Data, f.InnerData()...)

		hdrBuf = hdrBuf[8:]
	}

	return nil
}

const CompressedFileHeaderMagic = "EXTRHEAD"
const CompressedFileFooterMagic = "EXTRFOOT"

type CompressedFile struct {
	Header       CompressedFileHeader
	Data         []byte
	Decompressed []byte
	Child        *CompressedFile
}

func (f *CompressedFile) InnerData() []byte {
	if f.Child != nil {
		return f.Child.InnerData()
	}
	return f.Decompressed
}

func (f *CompressedFile) ToBuffer() (buf []byte, err error) {
	buf, err = binary.Append(buf, binary.LittleEndian, f.Header)
	if err != nil {
		return nil, err
	}
	buf = append(buf, f.Data...)
	// pad
	buf = append(buf, make([]byte, f.Header.FooterOffset-uint32(64+len(f.Data)))...)
	buf = append(buf, []byte(CompressedFileFooterMagic)...)
	return buf, nil
}

func (f *CompressedFile) FromBuffer(buf []byte) (err error) {
	// multiple segments
	if _, err := binary.Decode(buf, binary.LittleEndian, &f.Header); err != nil {
		return err
	}

	if !bytes.Equal(buf[:len(CompressedFileHeaderMagic)], []byte(CompressedFileHeaderMagic)) {
		return errors.New("expected EXTRHEAD, not found")
	}

	if !bytes.Equal(buf[f.Header.FooterOffset:f.Header.FooterOffset+uint32(len(CompressedFileFooterMagic))], []byte(CompressedFileFooterMagic)) {
		return errors.New("expected EXTRFOOT, not found")
	}
	f.Data = buf[64 : 64+f.Header.SourceSize]

	if f.Header.CompressedFlag != 1 {
		return errors.New("expected compressed flag, got 0")
	}

	switch CompressionType(f.Header.CompressionType) {
	case CompressionTypeLZSS:
		f.Decompressed, err = lzss.Decompress(f.Data, 0)
		if err != nil {
			return err
		}
	case CompressionTypeGZIP:
		gz, err := gzip.NewReader(bytes.NewReader(f.Data))
		if err != nil {
			return err
		}
		defer gz.Close()
		f.Decompressed, err = io.ReadAll(gz)
		if err != nil {
			return err
		}
	case CompressionTypeNone:
		f.Decompressed = buf
	default:
		return errors.New("invalid compression type")
	}

	if uint32(len(f.Decompressed)) != f.Header.DestinationSize {
		return fmt.Errorf("expected destination size %d, got %d", f.Header.DestinationSize, len(f.Decompressed))
	}

	var cksumBuf []byte

	for i := 0; i < len(f.Decompressed); i += int(f.Header.ChecksumSkip) {
		cksumBuf = append(cksumBuf, f.Decompressed[i])
	}

	if adler32.Checksum(cksumBuf) != f.Header.Checksum {
		return ErrInvalidDataChecksum
	}

	if bytes.Equal(f.Decompressed[:len(CompressedFileHeaderMagic)], []byte(CompressedFileHeaderMagic)) {
		var child CompressedFile
		if err = child.FromBuffer(f.Decompressed); err != nil {
			return fmt.Errorf("could not decompress children: %w", err)
		}
		f.Child = &child
	}

	return nil
}

type CompressedFileHeader struct {
	Magic [8]byte
	// Unknown DRV\x20
	Unknown         [4]byte
	CompressedFlag  uint16
	CompressionType uint16
	// DestinationSize Decompressed size
	DestinationSize    uint32
	DestinationAddress uint32

	SourceSize    uint32
	SourceAddress uint32
	FooterOffset  uint32

	Unknown2 [4]byte

	// Checksum adler32 calculated every checksum_skip bytes of decompressed data
	Checksum     uint32
	ChecksumSkip uint32
	Unused       [16]byte
}
