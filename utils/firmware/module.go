package firmware

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/adler32"
	"io"
	"slices"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/crypto"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
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

var PanasonicHeader = [12]byte([]byte("PANASONIC\x00\x00\x00"))

func findKey(block [8]byte, pattern []byte) (key crypto.CustomKey, ok bool) {
	for _, k := range crypto.CustomKeys {
		testBuf := block
		expanded := k.Expand()
		expanded.Decrypt(testBuf[:])
		if bytes.Equal(testBuf[:len(pattern)], pattern) {
			return k, true
		}
	}
	return crypto.CustomKey{}, false
}

func findKeySlow(block []byte) (key crypto.CustomKey, ok bool) {
	offset := -1
	var bestScore int
	for i, k := range crypto.CustomKeys {
		testBuf := slices.Clone(block)
		expanded := k.Expand()
		expanded.Decrypt(testBuf[:])
		score := bytes.Count(testBuf, []byte{0x00, 0x00, 0x00, 0x00})
		score += bytes.Count(testBuf, []byte{0xFF, 0xFF, 0xFF, 0xFF})
		if score > bestScore {
			bestScore = score
			offset = i
		}
	}
	if offset == -1 {
		return crypto.CustomKey{}, false
	}
	return crypto.CustomKeys[offset], true
}

func findAESKey(block []byte, pattern []byte) (key crypto.AESKey, ok bool) {
	for _, k := range crypto.AESKeys {
		testBuf := bytes.Clone(block)
		k.DecryptCustom(testBuf)
		if bytes.Equal(testBuf[32:32+len(pattern)], pattern) {
			return k, true
		}
	}
	return crypto.AESKey{}, false
}

type ModuleEntries struct {
	Header          []byte
	Entries         []*Module
	Offset          int
	HeaderEncrypted bool
	AESKey          *crypto.AESKey
	CustomKey       crypto.CustomKey
	ExpandedKey     *crypto.ExpandedCustomKey
	Sum             func(data []byte) uint32
}

type AESHeader struct {
	Entries [4]AESHeaderFileEntry
	Magic   [12]byte
	Sum     uint32
}

type AESHeaderFileEntry struct {
	Offset uint32
	Size   uint32
}

func (m *ModuleEntries) FromBuffer(buf []byte) (err error) {
	if len(buf) < FileHeaderSize {
		return errors.New("FRM file too short")
	}

	m.Sum = adler32.Checksum

	// AES-class firmware
	if aesKey, ok := findAESKey(buf[:64], PanasonicHeader[:]); ok {

		m.AESKey = &aesKey
		m.CustomKey = m.AESKey.Custom

		// decrypt header
		m.AESKey.DecryptCustom(buf[:48])
		var header AESHeader

		if _, err = binary.Decode(buf[:48], binary.LittleEndian, &header); err != nil {
			return err
		}

		var sections []AESHeaderFileEntry

		for _, e := range header.Entries {
			if e.Offset != 0 && e.Size != 0 {
				sections = append(sections, e)
			}
			m.AESKey.Decrypt(buf[e.Offset : e.Offset+e.Size])
		}

		if len(sections) != 1 {
			return errors.New("AES header with not one entry not supported")
		}

		e := sections[0]

		if e.Offset != 48 || header.Magic != PanasonicHeader {
			return errors.New("AES header with non standard offset not supported")
		}

		sum := m.Sum(buf[:44])
		if sum != header.Sum {
			return ErrInvalidHeaderChecksum
		}

		// advance
		buf = buf[e.Offset:]

		//TODO: AES files with also multiple firmware files

	}

	m.HeaderEncrypted = true
	m.Offset = 0

	if bytes.HasPrefix(buf, PanasonicHeader[:]) {
		sum := m.Sum(buf[:44])
		if sum == binary.LittleEndian.Uint32(buf[44:]) {
			m.HeaderEncrypted = buf[43] == 1
		}

		m.Offset = 48
	}
	var ok bool

	if m.HeaderEncrypted {
		block := [8]byte(buf[m.Offset : m.Offset+8])
		if m.CustomKey, ok = findKey(block, []byte("PROG0.00")); ok {

		} else if m.CustomKey, ok = findKey(block, []byte("PROG")); ok {

		} else if m.CustomKey, ok = findKeySlow(buf[m.Offset:]); ok {

		} else {
			return errors.New("encryption key not found")
		}
	} else if m.CustomKey, ok = findKeySlow(buf[m.Offset:]); ok {

	} else {
		//TODO
		return errors.New("encryption key not found: unencrypted header")
	}

	m.ExpandedKey = new(m.CustomKey.Expand())

	if m.HeaderEncrypted && m.ExpandedKey != nil {
		m.ExpandedKey.Decrypt(buf[m.Offset:FileHeaderSize])
	}

	// old firmware
	if m.Offset == 0 && m.HeaderEncrypted && m.AESKey == nil {
		m.Sum = utils.Checksum32
	}

	m.Header = buf[:FileHeaderSize]

	r := bytes.NewReader(buf)

	for {
		var mod Module
		if err := mod.FromReader(r, m.Sum, m.ExpandedKey); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			m.Entries = append(m.Entries, nil)
			if slices.Contains(KnownModuleHeaderNames, mod.Header.Name) && mod.Header.OEM == OEMPanasonic {
				//TODO: handle false positives?
				return err
			}
			continue
		}
		m.Entries = append(m.Entries, &mod)
	}
	return nil
}

func (m *ModuleEntries) ToBuffer() (buf []byte, err error) {

	const alignSize = 0x1000

	offset := utils.Align(uint32(FileHeaderSize), alignSize)
	for _, mod := range m.Entries {
		if mod == nil {
			continue
		}
		mod.Header.Size = uint32(len(mod.Data))
		mod.Header.Offset = offset
		offset = utils.Align(offset+mod.Header.Size, alignSize)
	}

	w := utils.NewWriteSeeker()
	if err = w.Truncate(int64(offset)); err != nil {
		return nil, err
	}

	for i, mod := range m.Entries {
		if mod == nil {
			if _, err = w.Write(m.Header[i*48 : i*48+48]); err != nil {
				return nil, err
			}
		} else {
			if err = mod.ToWriter(w, m.Sum, m.ExpandedKey); err != nil {
				return nil, err
			}
		}
	}

	buf = w.Bytes()

	if m.HeaderEncrypted {
		m.ExpandedKey.Encrypt(buf[m.Offset:FileHeaderSize])
	}

	if m.AESKey != nil {
		// encrypt file data
		m.AESKey.Encrypt(buf)

		header := AESHeader{
			Entries: [4]AESHeaderFileEntry{
				{
					Offset: 48,
					Size:   uint32(len(buf)),
				},
			},
			Magic: PanasonicHeader,
			Sum:   0,
		}
		hdrBuf, err := binary.Append(nil, binary.LittleEndian, header)
		if err != nil {
			return nil, err
		}
		// put sum
		binary.LittleEndian.PutUint32(hdrBuf[44:], m.Sum(hdrBuf[:44]))
		// encrypt header
		m.AESKey.EncryptCustom(hdrBuf)
		buf = append(hdrBuf, buf...)
	}

	return buf, nil
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

func (m *Module) ToWriter(w io.WriteSeeker, sumFunc func([]byte) uint32, key *crypto.ExpandedCustomKey) (err error) {
	m.Header.Size = uint32(len(m.Data))

	m.Header.DataSum = sumFunc(m.Data)

	if m.Header.Name != ModuleNameMAIN && key != nil {
		key.Encrypt(m.Data)
	}

	var hdrBuf [48]byte

	if _, err = binary.Encode(hdrBuf[:], binary.LittleEndian, &m.Header); err != nil {
		return err
	}

	// put checksum
	binary.LittleEndian.PutUint32(hdrBuf[len(hdrBuf)-4:], sumFunc(hdrBuf[:len(hdrBuf)-4]))

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

func (m *Module) FromReader(r io.ReadSeeker, sumFunc func([]byte) uint32, key *crypto.ExpandedCustomKey) (err error) {
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

	if sum := sumFunc(hdrBuf[:len(hdrBuf)-4]); sum != m.Header.HeaderSum {
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

	if m.Header.Name != [4]byte([]byte("MAIN")) && key != nil {
		key.Decrypt(m.Data)
	}

	if sumFunc(m.Data) != m.Header.DataSum {
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
