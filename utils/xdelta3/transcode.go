package xdelta3

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/ulikunitz/xz"
)

const (
	headerDecompress = 0x01
	headerCodeTable  = 0x02
	headerAppHeader  = 0x04
)

const (
	windowSource  = 0x01
	windowTarget  = 0x02
	windowAdler32 = 0x04
)

const (
	deltaDataComp = 0x01
	deltaInstComp = 0x02
	deltaAddrComp = 0x04
)

const (
	compressorDJW  = 1
	compressorLZMA = 2
	compressorFGK  = 16
)

var (
	ErrBadMagic    = errors.New("bad magic")
	ErrUnsupported = errors.New("unsupported feature")
)

type transcoder struct {
	r                   *bytes.Reader
	secondaryCompressor byte
	windowNum           int
}

func (t *transcoder) rewriteHeader(out *bytes.Buffer) error {
	var magic [4]byte
	if _, err := io.ReadFull(t.r, magic[:]); err != nil {
		return io.ErrUnexpectedEOF
	}
	if magic[0] != 0xD6 || magic[1] != 0xC3 || magic[2] != 0xC4 {
		return ErrBadMagic
	}
	if magic[3] != 0x00 {
		return fmt.Errorf("%w: version %d unsupported", ErrUnsupported, magic[3])
	}
	ind, err := t.r.ReadByte()
	if err != nil {
		return io.ErrUnexpectedEOF
	}

	var (
		compressorId            byte
		appHeaderSize           uint32
		hasSecondaryCompression = ind&headerDecompress != 0
		hasCodeTable            = ind&headerCodeTable != 0
		hasApplicationHeader    = ind&headerAppHeader != 0
	)

	if hasSecondaryCompression {
		if compressorId, err = t.r.ReadByte(); err != nil {
			return io.ErrUnexpectedEOF
		}
		if err := checkSecondaryCompressor(compressorId); err != nil {
			return err
		}
		t.secondaryCompressor = compressorId
	}
	if hasCodeTable {
		// ably/vcdiff-go uses only the default table.
		return fmt.Errorf("%w: custom code table", ErrUnsupported)
	}
	if hasApplicationHeader {
		if appHeaderSize, err = readVarInt(t.r); err != nil {
			return err
		}
		if _, err := t.r.Seek(int64(appHeaderSize), io.SeekCurrent); err != nil {
			return io.ErrUnexpectedEOF
		}
	}

	out.Write([]byte{0xD6, 0xC3, 0xC4, 0x00, 0x00})
	return nil
}

func (t *transcoder) rewriteWindow(out *bytes.Buffer) error {
	t.windowNum++
	winInd, err := t.r.ReadByte()
	if err != nil {
		return io.ErrUnexpectedEOF
	}

	var srcSize, srcPos uint32
	hasSrc := winInd&(windowSource|windowTarget) != 0
	if hasSrc {
		if srcSize, err = readVarInt(t.r); err != nil {
			return err
		}
		if srcPos, err = readVarInt(t.r); err != nil {
			return err
		}
	}

	// discard old delta-encoding length
	if _, err = readVarInt(t.r); err != nil {
		return err
	}
	tgtLen, err := readVarInt(t.r)
	if err != nil {
		return err
	}
	deltaInd, err := t.r.ReadByte()
	if err != nil {
		return io.ErrUnexpectedEOF
	}
	dataLen, err := readVarInt(t.r)
	if err != nil {
		return err
	}
	instLen, err := readVarInt(t.r)
	if err != nil {
		return err
	}
	addrLen, err := readVarInt(t.r)
	if err != nil {
		return err
	}

	var adlerSum [4]byte
	hasAdler := winInd&windowAdler32 != 0
	if hasAdler {
		if _, err := io.ReadFull(t.r, adlerSum[:]); err != nil {
			return io.ErrUnexpectedEOF
		}
	}

	data := make([]byte, dataLen)
	if _, err := io.ReadFull(t.r, data); err != nil {
		return io.ErrUnexpectedEOF
	}
	inst := make([]byte, instLen)
	if _, err := io.ReadFull(t.r, inst); err != nil {
		return io.ErrUnexpectedEOF
	}
	addr := make([]byte, addrLen)
	if _, err := io.ReadFull(t.r, addr); err != nil {
		return io.ErrUnexpectedEOF
	}

	if t.secondaryCompressor != 0 {
		if data, err = t.maybeDecompress(deltaInd&deltaDataComp != 0, data, "data"); err != nil {
			return err
		}
		if inst, err = t.maybeDecompress(deltaInd&deltaInstComp != 0, inst, "inst"); err != nil {
			return err
		}
		if addr, err = t.maybeDecompress(deltaInd&deltaAddrComp != 0, addr, "addr"); err != nil {
			return err
		}
	}

	// inner block
	var inner bytes.Buffer
	writeVarInt(&inner, tgtLen)
	// delta indicator: secondary stripped
	inner.WriteByte(0x00)
	writeVarInt(&inner, uint32(len(data)))
	writeVarInt(&inner, uint32(len(inst)))
	writeVarInt(&inner, uint32(len(addr)))
	if hasAdler {
		inner.Write(adlerSum[:])
	}
	inner.Write(data)
	inner.Write(inst)
	inner.Write(addr)

	out.WriteByte(winInd)
	if hasSrc {
		writeVarInt(out, srcSize)
		writeVarInt(out, srcPos)
	}
	writeVarInt(out, uint32(inner.Len()))
	out.Write(inner.Bytes())
	return nil
}

func (t *transcoder) maybeDecompress(flag bool, in []byte, label string) ([]byte, error) {
	if !flag {
		return in, nil
	}
	out, err := decodeSection(t.secondaryCompressor, in)
	if err != nil {
		return nil, fmt.Errorf("%s section: %w", label, err)
	}
	return out, nil
}

func checkSecondaryCompressor(id byte) error {
	switch id {
	case compressorDJW, compressorLZMA, compressorFGK:
		return nil
	}
	return fmt.Errorf("%w: secondary id %d", ErrUnsupported, id)
}

// decodeSection reads the per-section size prefix and decompresses the remaining bytes
func decodeSection(id byte, in []byte) ([]byte, error) {
	r := bytes.NewReader(in)
	outSize, err := readVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("size prefix: %w", err)
	}
	switch id {
	case compressorLZMA:
		xr, err := xz.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("xz init: %w", err)
		}
		out := make([]byte, outSize)
		if _, err := io.ReadFull(xr, out); err != nil {
			return nil, fmt.Errorf("xz read %d: %w", outSize, err)
		}
		return out, nil
	case compressorDJW:
		return nil, fmt.Errorf("%w: DJW not implemented", ErrUnsupported)
	case compressorFGK:
		return nil, fmt.Errorf("%w: FGK not implemented", ErrUnsupported)
	}
	return nil, fmt.Errorf("%w: secondary id %d", ErrUnsupported, id)
}

func readVarInt(r *bytes.Reader) (uint32, error) {
	var v uint32
	for range 5 {
		b, err := r.ReadByte()
		if err != nil {
			return 0, io.ErrUnexpectedEOF
		}
		v = (v << 7) | uint32(b&0x7F)
		if b&0x80 == 0 {
			return v, nil
		}
	}
	return 0, errors.New("varint overflow")
}

func writeVarInt(w io.ByteWriter, v uint32) {
	var buf [5]byte
	i := len(buf) - 1
	buf[i] = byte(v & 0x7F)
	for v >>= 7; v > 0; v >>= 7 {
		i--
		buf[i] = byte(v&0x7F) | 0x80
	}
	for ; i < len(buf); i++ {
		_ = w.WriteByte(buf[i])
	}
}
