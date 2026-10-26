package firmware

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/gzip"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/crypto"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/lzss"
	"golang.org/x/sync/errgroup"
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
	// DecompressedSegmentSize The maximum size of each decompressed part.
	// Use ModernMainSegmentTargetSize for most firmware, though some legacy ones use LegacyMainSegmentTargetSize
	// This also selects the encrypted size
	DecompressedSegmentSize uint32
	CompressionType         CompressionType
}

const ModernMainSegmentEncryptedSize = 0x1400
const LegacyMainSegmentEncryptedSize = 0x2800
const ModernMainSegmentTargetSize = 16 * 1024 * 1024
const LegacyMainSegmentTargetSize = 32 * 1024 * 1024

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

func NewMainListWithCompression(data []byte, sumFunc func([]byte) uint32, key *crypto.ExpandedCustomKey, targetSegmentSize uint32, compressionType CompressionType, template *MainList) (*MainList, error) {
	if bits.OnesCount32(uint32(compressionType)) != 1 {
		return nil, errors.New("compression type is not supported")
	}

	var out MainList
	out.Header.Unknown1 = 1

	unknown := [4]byte([]byte("DRV "))
	var unknown2 [4]byte
	if template != nil && len(template.Segments) > 0 {
		out.Header.Unknown1 = template.Header.Unknown1
		unknown = template.Segments[0].Header.Unknown
		unknown2 = template.Segments[0].Header.Unknown2
	}

	out.Header.ListSize = 20
	out.Header.DecompressedSegmentSize = targetSegmentSize
	out.Header.CompressionType = compressionType
	out.Data = data

	segmentSize := int(targetSegmentSize)

	var wg errgroup.Group
	out.Segments = make([]CompressedFile, utils.Align(len(data), segmentSize)/segmentSize)
	out.Entries = make([]MainListEntry, len(out.Segments))

	for offset := 0; offset < len(data); offset += segmentSize {
		wg.Go(func() error {

			partNumber := offset / segmentSize

			part := data[offset:min(offset+segmentSize, len(data))]

			// this value is arbitrary, but it's what is used on current firmware
			const checksumSkip = 16

			var cksumBuf []byte

			for i := 0; i < len(part); i += checksumSkip {
				cksumBuf = append(cksumBuf, part[i])
			}

			var compressed []byte

			switch compressionType {
			case CompressionTypeGZIP:
				if err := func() error {
					wBuf := bytes.NewBuffer(nil)
					w, err := gzip.NewWriterLevel(wBuf, gzip.BestCompression)
					if err != nil {
						return err
					}
					defer w.Close()

					// set flags as original, except time
					w.Header.ModTime = time.Now().UTC()
					w.Header.OS = 3
					// pattern seen on regular firmware
					w.Header.Name = fmt.Sprintf("bank.rom.in%d.s", partNumber)

					if _, err = w.Write(part); err != nil {
						return err
					}
					if err = w.Flush(); err != nil {
						return err
					}
					if err = w.Close(); err != nil {
						return err
					}

					compressed = wBuf.Bytes()

					return nil
				}(); err != nil {
					return err
				}
			case CompressionTypeLZSS:
				if err := func() (err error) {
					compressed, err = lzss.Compress(part, true)
					return err
				}(); err != nil {
					return err
				}
			default:
				return errors.New("unknown compression type")
			}

			// align to minimum known supported decryption
			padded := max(len(compressed), out.encryptedSize()-64)

			if padded%8 != 0 {
				padded += 8 - padded%8
			}

			segment := CompressedFile{
				Header: CompressedFileHeader{
					Magic: [8]byte([]byte(CompressedFileHeaderMagic)),
					// Doesn't seem used, TODO: is this magic different elsewhere?
					Unknown:            unknown,
					CompressedFlag:     1,
					CompressionType:    uint16(compressionType),
					DestinationSize:    uint32(len(part)),
					DestinationAddress: 0,
					SourceSize:         uint32(len(compressed)),
					//TODO??? both source and destination address are overwritten by flasher before usage
					SourceAddress: 0,
					FooterOffset:  64 + uint32(padded),
					Unknown2:      unknown2,
					Checksum:      sumFunc(cksumBuf),
					ChecksumSkip:  checksumSkip,
					Unused:        [16]byte{},
				},
				RawData:      compressed,
				Decompressed: part,
				Child:        nil,
			}

			// encode and encrypt to calculate checksums
			outBuf, err := segment.ToBuffer()
			if err != nil {
				return err
			}

			if len(outBuf) < out.encryptedSize() {
				return errors.New("encrypted block too small")
			} else if key != nil {
				// yep, this can overlap
				// done in reverse
				key.Encrypt(outBuf[len(outBuf)-out.encryptedSize():])
				key.Encrypt(outBuf[:out.encryptedSize()])
			}

			out.Entries[partNumber] = MainListEntry{
				Size:     uint32(len(outBuf)),
				Checksum: sumFunc(outBuf),
			}

			// apply unencrypted segments here
			out.Segments[partNumber] = segment
			atomic.AddUint32(&out.Header.ListSize, 8)

			return nil
		})
	}

	err := wg.Wait()
	if err != nil {
		return nil, err
	}

	return &out, nil
}

func (l *MainList) encryptedSize() int {
	if l.Header.DecompressedSegmentSize == LegacyMainSegmentTargetSize {
		return LegacyMainSegmentEncryptedSize
	} else if l.Header.DecompressedSegmentSize == ModernMainSegmentTargetSize {
		return ModernMainSegmentEncryptedSize
	} else {
		// TODO?
		return ModernMainSegmentEncryptedSize
	}
	//TODO: handle old legacy parts with 102400 offset along with FooterOffset+8 anchor for encryption
}

func (l *MainList) ToBuffer(sumFunc func([]byte) uint32, key *crypto.ExpandedCustomKey) (buf []byte, err error) {
	hdrBuf := make([]byte, l.Header.ListSize)

	if _, err = binary.Encode(hdrBuf[:20], binary.LittleEndian, l.Header); err != nil {
		return nil, err
	}

	for i, e := range l.Entries {
		if _, err = binary.Encode(hdrBuf[20+i*8:], binary.LittleEndian, e); err != nil {
			return nil, err
		}
	}

	binary.LittleEndian.PutUint32(hdrBuf, sumFunc(hdrBuf[4:]))
	buf = append(buf, hdrBuf...)

	for _, s := range l.Segments {
		part, err := s.ToBuffer()
		if err != nil {
			return nil, err
		}

		if len(part) < l.encryptedSize() {
			//TODO handle this across overlap, versions
			return nil, errors.New("encrypted block too small")
		} else if key != nil {
			//yep, this can overlap
			key.Encrypt(part[len(part)-l.encryptedSize():])
			key.Encrypt(part[:l.encryptedSize()])
		}
		buf = append(buf, part...)
	}

	return buf, nil
}

func (l *MainList) FromReader(r io.ReadSeeker, sumFunc func([]byte) uint32, key *crypto.ExpandedCustomKey) (err error) {
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

	if sum := sumFunc(hdrBuf[4:]); sum != l.Header.Checksum {
		return fmt.Errorf("%08x != %08x: %w", sum, l.Header.Checksum, ErrInvalidHeaderChecksum)
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
		if sum := sumFunc(buf); sum != e.Checksum {
			return fmt.Errorf("%08x != %08x: %w", sum, e.Checksum, ErrInvalidDataChecksum)
		}

		if len(buf) < l.encryptedSize() {
			return errors.New("encrypted block too small")
		} else if key != nil {
			//yep, this can overlap
			key.Decrypt(buf[:l.encryptedSize()])
			key.Decrypt(buf[len(buf)-l.encryptedSize():])
		}

		var f CompressedFile
		if err = f.FromBuffer(buf, sumFunc); err != nil {
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
	Header CompressedFileHeader
	// RawData Raw encoded data
	RawData      []byte
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
	buf = append(buf, f.RawData...)
	// pad
	buf = append(buf, make([]byte, f.Header.FooterOffset-uint32(64+len(f.RawData)))...)
	buf = append(buf, []byte(CompressedFileFooterMagic)...)
	return buf, nil
}

func (f *CompressedFile) FromBuffer(buf []byte, sumFunc func([]byte) uint32) (err error) {
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
	f.RawData = buf[64 : 64+f.Header.SourceSize]

	if f.Header.CompressedFlag != 1 {
		return errors.New("expected compressed flag, got 0")
	}

	switch CompressionType(f.Header.CompressionType) {
	case CompressionTypeLZSS:
		f.Decompressed, err = lzss.Decompress(f.RawData, 0, int(f.Header.DestinationSize))
		if err != nil {
			return err
		}
	case CompressionTypeGZIP:
		gz, err := gzip.NewReader(bytes.NewReader(f.RawData))
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

	if sum := sumFunc(cksumBuf); sum != f.Header.Checksum {
		return fmt.Errorf("%08x != %08x: %w", sum, f.Header.Checksum, ErrInvalidDataChecksum)
	}

	if bytes.Equal(f.Decompressed[:len(CompressedFileHeaderMagic)], []byte(CompressedFileHeaderMagic)) {
		var child CompressedFile
		if err = child.FromBuffer(f.Decompressed, sumFunc); err != nil {
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

	// Checksum calculated every checksum_skip bytes of decompressed data
	Checksum     uint32
	ChecksumSkip uint32
	Unused       [16]byte
}
