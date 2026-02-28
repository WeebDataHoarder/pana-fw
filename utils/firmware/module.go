package firmware

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/adler32"
	"io"
	"slices"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/crypto"
)

var ModuleNameMAIN = [4]byte([]byte("MAIN"))

var KnownModuleHeaderNames = [][4]byte{
	[4]byte([]byte("PROG")),
	[4]byte([]byte("LOSD")),
	[4]byte([]byte("DRV1")),
	[4]byte([]byte("BOOT")),
	[4]byte([]byte("MINI")),
	[4]byte([]byte("BUCD")),
	[4]byte([]byte("SCRN")),
	[4]byte([]byte("RFM ")),
	[4]byte([]byte("ELSW")),
	[4]byte([]byte("MAIN")),
	[4]byte([]byte("TPNL")),
	[4]byte([]byte("GSTR")),
	[4]byte([]byte("LCD ")),
	[4]byte([]byte("TIM ")),
	[4]byte([]byte("TIMI")),
	[4]byte([]byte("TIME")),
	[4]byte([]byte("CDDB")),
	[4]byte([]byte("VGD ")),
	[4]byte([]byte("HDD1")),
	[4]byte([]byte("HDD2")),
}

type Module struct {
	Header ModuleHeader

	Data []byte
}

const FileHeaderSize = 0x1800

const OEMPanasonic byte = 'M'

var ErrInvalidHeaderChecksum = errors.New("invalid header checksum")
var ErrInvalidDataChecksum = errors.New("invalid data checksum")
var ErrNotModule = errors.New("not a module")

func (m *Module) ToWriter(w io.WriteSeeker, key [32]byte) (err error) {
	m.Header.Size = uint32(len(m.Data))

	m.Header.DataSum = adler32.Checksum(m.Data)

	if m.Header.Name != ModuleNameMAIN {
		crypto.CustomEncrypt(m.Data[:], key)
	}

	var hdrBuf [48]byte

	if _, err = binary.Encode(hdrBuf[:], binary.LittleEndian, &m.Header); err != nil {
		return err
	}

	// put checksum
	binary.LittleEndian.PutUint32(hdrBuf[len(hdrBuf)-4:], adler32.Checksum(hdrBuf[:len(hdrBuf)-4]))

	if _, err = w.Write(hdrBuf[:]); err != nil {
		return err
	}

	off, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	if off > FileHeaderSize {
		return io.EOF
	}

	if _, err = w.Seek(int64(m.Header.Offset), io.SeekStart); err != nil {
		return err
	}

	defer w.Seek(off, io.SeekStart)

	if _, err = w.Write(m.Data); err != nil {
		return err
	}

	return nil
}

func (m *Module) FromReader(r io.ReadSeeker, key [32]byte) (err error) {
	var hdrBuf [48]byte

	if _, err = io.ReadFull(r, hdrBuf[:]); err != nil {
		return err
	}

	off, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	if off > FileHeaderSize {
		return io.EOF
	}

	if _, err = binary.Decode(hdrBuf[:], binary.LittleEndian, &m.Header); err != nil {
		return err
	}

	if adler32.Checksum(hdrBuf[:len(hdrBuf)-4]) != m.Header.HeaderSum {
		return ErrInvalidHeaderChecksum
	}

	if m.Header.OEM != OEMPanasonic {
		return fmt.Errorf("invalid OEM %02x != %02x", m.Header.OEM, OEMPanasonic)
	}

	if !slices.Contains(KnownModuleHeaderNames, m.Header.Name) {
		return ErrNotModule
	}

	if _, err = r.Seek(int64(m.Header.Offset), io.SeekStart); err != nil {
		return err
	}

	defer r.Seek(off, io.SeekStart)

	m.Data = make([]byte, m.Header.Size)
	if _, err = io.ReadFull(r, m.Data); err != nil {
		return err
	}

	if m.Header.Name != [4]byte([]byte("MAIN")) {
		crypto.CustomDecrypt(m.Data[:], key)
	}

	if adler32.Checksum(m.Data) != m.Header.DataSum {
		return ErrInvalidDataChecksum
	}

	return nil
}

type ModuleHeader struct {
	// Name Module name
	Name [4]byte
	// Version Module version
	Version  [4]byte
	Unknown1 [4]byte
	// Offset Module data offset
	Offset uint32
	// Model Device Model ID
	Model [8]byte
	// OEM Device OEM ID, 0x4D = M for Panasonic
	OEM byte
	// FormatHDD If not 0, format HDD, and force update of module
	FormatHDD byte
	// Unknown2 Some sort of ID, purpose unknown (last 3 of the ID seems to be the main firm version)
	Unknown2 [6]byte
	// Size Module data size
	Size uint32
	// DataSum Module data Adler32 checksum
	DataSum  uint32
	Unknown3 [4]byte
	// HeaderSum Module entry Adler32 checksum (all bytes before this)
	HeaderSum uint32
}
