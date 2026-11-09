package elfpatch

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
)

const (
	pageSize     = 0x1000
	maxGrowAlign = 1 << 20
)

// Image is a mutable copy of a little-endian ELF32 ARM binary.
type Image struct {
	data []byte
	f    *elf.File

	segs      []segment
	text      addrRange
	rodata    []addrRange
	exports   map[string]uint32 // st_value; bit 0 selects Thumb
	plt       map[string]uint32 // imported name -> ARM PLT entry
	funcs     funcIndex
	haveFuncs bool
}

type addrRange struct{ start, end uint32 }

func (r addrRange) contains(va uint32) bool { return va >= r.start && va < r.end }

type segment struct {
	vaddr, memsz, off, filesz uint32
	exec, write               bool
	align                     uint32
	phdrOff                   uint32
}

func (s *segment) vaEnd() uint32   { return s.vaddr + s.filesz }
func (s *segment) fileEnd() uint32 { return s.off + s.filesz }

// Open validates data and returns an Image over a private copy of it.
func Open(data []byte) (*Image, error) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if f.Class != elf.ELFCLASS32 || f.ByteOrder != binary.LittleEndian || f.Machine != elf.EM_ARM {
		return nil, fmt.Errorf("only little-endian ELF32 ARM is supported, got %v/%v/%v", f.Class, f.ByteOrder, f.Machine)
	}
	im := &Image{data: slices.Clone(data), f: f, exports: map[string]uint32{}, plt: map[string]uint32{}}
	if err := im.loadSegments(); err != nil {
		return nil, err
	}
	im.loadRanges()
	syms, err := f.DynamicSymbols()
	if err != nil && !errors.Is(err, elf.ErrNoSymbols) {
		return nil, fmt.Errorf("dynamic symbols: %w", err)
	}
	for _, s := range syms {
		if s.Section != elf.SHN_UNDEF && s.Value != 0 && elf.ST_TYPE(s.Info) == elf.STT_FUNC {
			im.exports[s.Name] = uint32(s.Value)
		}
	}
	if err := im.indexPLT(syms); err != nil {
		return nil, fmt.Errorf("plt: %w", err)
	}
	return im, nil
}

func (im *Image) loadSegments() error {
	phoff := binary.LittleEndian.Uint32(im.data[28:])
	phentsize := binary.LittleEndian.Uint16(im.data[42:])
	size := uint64(len(im.data))
	for i, p := range im.f.Progs {
		if p.Type != elf.PT_LOAD {
			continue
		}
		if p.Filesz > p.Memsz || p.Off+p.Filesz > size || p.Vaddr+p.Memsz >= 1<<32 {
			return fmt.Errorf("invalid PT_LOAD at %#x", p.Vaddr)
		}
		im.segs = append(im.segs, segment{
			vaddr: uint32(p.Vaddr), memsz: uint32(p.Memsz),
			off: uint32(p.Off), filesz: uint32(p.Filesz),
			exec: p.Flags&elf.PF_X != 0, write: p.Flags&elf.PF_W != 0,
			align:   uint32(p.Align),
			phdrOff: phoff + uint32(i)*uint32(phentsize),
		})
	}
	exec := im.firstExec()
	if exec == nil {
		return errors.New("no executable PT_LOAD segment")
	}
	im.text = addrRange{exec.vaddr, exec.vaEnd()}
	return nil
}

func (im *Image) firstExec() *segment {
	for i := range im.segs {
		if im.segs[i].exec {
			return &im.segs[i]
		}
	}
	return nil
}

func (im *Image) loadRanges() {
	if t := im.f.Section(".text"); t != nil && im.execFileBacked(uint32(t.Addr), uint32(t.Addr+t.Size)) {
		im.text = addrRange{uint32(t.Addr), uint32(t.Addr + t.Size)}
	}
	for _, s := range im.f.Sections {
		if s.Flags&elf.SHF_ALLOC != 0 && s.Flags&(elf.SHF_WRITE|elf.SHF_EXECINSTR) == 0 &&
			s.Type == elf.SHT_PROGBITS && s.Size > 0 {
			im.rodata = append(im.rodata, addrRange{uint32(s.Addr), uint32(s.Addr + s.Size)})
		}
	}
	if len(im.f.Sections) > 0 {
		return
	}
	for _, s := range im.segs {
		if !s.exec && !s.write && s.filesz > 0 {
			im.rodata = append(im.rodata, addrRange{s.vaddr, s.vaEnd()})
		}
	}
}

// Bytes returns the file image, which aliases the Image's storage.
func (im *Image) Bytes() []byte { return im.data }

// TextRange returns the scanned executable region [start,end).
func (im *Image) TextRange() (uint32, uint32) { return im.text.start, im.text.end }

// segmentFor returns the segment whose file-backed bytes hold [va,va+n).
func (im *Image) segmentFor(va uint32, n uint64) *segment {
	for i := range im.segs {
		s := &im.segs[i]
		if va >= s.vaddr && uint64(va)+n <= uint64(s.vaEnd()) {
			return s
		}
	}
	return nil
}

// VAToOff maps a virtual address to its file offset.
func (im *Image) VAToOff(va uint32) (uint32, bool) {
	s := im.segmentFor(va, 1)
	if s == nil {
		return 0, false
	}
	return s.off + (va - s.vaddr), true
}

// ReadVA returns the n bytes at va, or nil unless a single segment backs them.
func (im *Image) ReadVA(va uint32, n int) []byte {
	if n <= 0 {
		return nil
	}
	s := im.segmentFor(va, uint64(n))
	if s == nil {
		return nil
	}
	off := s.off + (va - s.vaddr)
	return im.data[off : off+uint32(n)]
}

// WriteVA overwrites the bytes at va, which must lie within one segment.
func (im *Image) WriteVA(va uint32, b []byte) error {
	dst := im.ReadVA(va, len(b))
	if dst == nil {
		return fmt.Errorf("write of %d bytes at VA %#x is not file-backed", len(b), va)
	}
	copy(dst, b)
	return nil
}

func (im *Image) mapped(va uint32) bool {
	return slices.ContainsFunc(im.segs, func(s segment) bool {
		return va >= s.vaddr && uint64(va) < uint64(s.vaddr)+uint64(s.memsz)
	})
}

func (im *Image) execFileBacked(start, end uint32) bool {
	if end <= start {
		return false
	}
	s := im.segmentFor(start, uint64(end-start))
	return s != nil && s.exec
}

func (im *Image) insnAt(va uint32) (Insn, bool) {
	code := im.ReadVA(va, 4)
	if code == nil {
		code = im.ReadVA(va, 2)
	}
	return Decode(code, va)
}

// GrowExecSlack extends the first executable segment over the zero padding that
// follows it in the file and returns the newly mapped span. Only p_filesz and
// p_memsz change. The span stops before anything else stored in the file and
// before the page the next segment is mapped into.
func (im *Image) GrowExecSlack(minSize uint32) (start, end uint32, err error) {
	s := im.firstExec()
	if s.memsz != s.filesz {
		return 0, 0, fmt.Errorf("exec segment has a zero-initialised tail (memsz %#x, filesz %#x)", s.memsz, s.filesz)
	}
	fileLimit := uint32(len(im.data))
	vaLimit := ^uint32(0)
	for _, l := range im.segs {
		if l.off >= s.fileEnd() {
			fileLimit = min(fileLimit, l.off)
		}
		if l.vaddr >= s.vaEnd() {
			vaLimit = min(vaLimit, max(l.vaddr&^(pageSize-1), s.vaEnd()))
		}
	}
	if shoff := binary.LittleEndian.Uint32(im.data[32:]); shoff >= s.fileEnd() {
		fileLimit = min(fileLimit, shoff)
	}
	for _, sec := range im.f.Sections {
		if sec.Type != elf.SHT_NOBITS && sec.Type != elf.SHT_NULL && sec.Size > 0 && sec.Offset >= uint64(s.fileEnd()) {
			fileLimit = uint32(min(uint64(fileLimit), sec.Offset))
		}
	}
	limit := min(fileLimit-s.fileEnd(), vaLimit-s.vaEnd())
	var avail uint32
	for avail < limit && im.data[s.fileEnd()+avail] == 0 {
		avail++
	}
	if avail == 0 || avail < minSize {
		return 0, 0, fmt.Errorf("exec segment has %d bytes of tail slack, need %d", avail, max(minSize, 1))
	}
	start = s.vaEnd()
	s.filesz += avail
	s.memsz = s.filesz
	binary.LittleEndian.PutUint32(im.data[s.phdrOff+16:], s.filesz)
	binary.LittleEndian.PutUint32(im.data[s.phdrOff+20:], s.memsz)
	return start, start + avail, nil
}

// ExtendExec makes at least minSize bytes of new mapped space after the first
// executable segment by inserting zeros into the file at its end and extending
// it into the unmapped addresses that follow. Virtual addresses do not change;
// only the file offsets in the program and section headers do. The insert is a
// multiple of the largest alignment of the segments it moves, as the loader
// requires offsets to stay congruent with addresses.
func (im *Image) ExtendExec(minSize uint32) (start, end uint32, err error) {
	s := im.firstExec()
	if s.memsz != s.filesz {
		return 0, 0, fmt.Errorf("exec segment has a zero-initialised tail (memsz %#x, filesz %#x)", s.memsz, s.filesz)
	}
	if s.filesz == 0 {
		return 0, 0, errors.New("exec segment is empty")
	}
	fileEnd := s.fileEnd()
	insertAlign, vaLimit := uint32(pageSize), ^uint32(0)
	for _, l := range im.segs {
		if l.off >= fileEnd {
			if l.align&(l.align-1) != 0 {
				return 0, 0, fmt.Errorf("segment at %#x has invalid alignment %#x", l.vaddr, l.align)
			}
			insertAlign = max(insertAlign, l.align)
		}
		if l.vaddr >= s.vaEnd() {
			vaLimit = min(vaLimit, max(l.vaddr&^(pageSize-1), s.vaEnd()))
		}
	}
	if insertAlign > maxGrowAlign {
		return 0, 0, fmt.Errorf("segment alignment %#x is too large to grow by", insertAlign)
	}
	insert := alignUp(minSize, insertAlign)
	grant := min(insert, vaLimit-s.vaEnd())
	if grant < minSize {
		return 0, 0, fmt.Errorf("exec segment can grow by %d bytes before the next segment, need %d", grant, minSize)
	}
	if uint64(len(im.data))+uint64(insert) >= 1<<32 {
		return 0, 0, fmt.Errorf("image would exceed 4 GiB")
	}
	if err := im.insertFileSpace(fileEnd, insert); err != nil {
		return 0, 0, err
	}
	for i := range im.segs {
		if l := &im.segs[i]; l != s && l.off >= fileEnd {
			l.off += insert
		}
	}
	start = s.vaEnd()
	s.filesz += grant
	s.memsz = s.filesz
	binary.LittleEndian.PutUint32(im.data[s.phdrOff+16:], s.filesz)
	binary.LittleEndian.PutUint32(im.data[s.phdrOff+20:], s.memsz)
	return start, start + grant, nil
}

// insertFileSpace adds insert zero bytes at file offset off and updates the
// headers, refusing anything stored across that point.
func (im *Image) insertFileSpace(off, insert uint32) error {
	le := binary.LittleEndian
	type fileRef struct{ field, offset, size uint32 }
	var refs []fileRef
	phoff, phentsize := le.Uint32(im.data[28:]), uint32(le.Uint16(im.data[42:]))
	if uint64(phoff)+uint64(len(im.f.Progs))*uint64(phentsize) > uint64(off) {
		return errors.New("program headers are not before the end of the exec segment")
	}
	for i := range uint32(len(im.f.Progs)) {
		ph := phoff + i*phentsize
		refs = append(refs, fileRef{ph + 4, le.Uint32(im.data[ph+4:]), le.Uint32(im.data[ph+16:])})
	}
	if shoff := le.Uint32(im.data[32:]); shoff != 0 {
		shentsize := uint32(le.Uint16(im.data[46:]))
		refs = append(refs, fileRef{32, shoff, uint32(len(im.f.Sections)) * shentsize})
		for i := range uint32(len(im.f.Sections)) {
			sh := shoff + i*shentsize
			size := le.Uint32(im.data[sh+20:])
			if elf.SectionType(le.Uint32(im.data[sh+4:])) == elf.SHT_NOBITS {
				size = 0
			}
			refs = append(refs, fileRef{sh + 16, le.Uint32(im.data[sh+16:]), size})
		}
	}
	for _, r := range refs {
		if r.offset < off && uint64(r.offset)+uint64(r.size) > uint64(off) {
			return fmt.Errorf("file range at %#x spans the end of the exec segment", r.offset)
		}
		if r.offset >= off && uint64(r.offset)+uint64(insert) >= 1<<32 {
			return fmt.Errorf("file offset %#x cannot move by %#x", r.offset, insert)
		}
	}
	im.data = slices.Insert(im.data, int(off), make([]byte, insert)...)
	for _, r := range refs {
		if r.offset < off {
			continue
		}
		field := r.field
		if field >= off {
			field += insert
		}
		le.PutUint32(im.data[field:], r.offset+insert)
	}
	f, err := elf.NewFile(bytes.NewReader(im.data))
	if err != nil {
		return fmt.Errorf("after growing: %w", err)
	}
	im.f = f
	return nil
}

// ExportVA returns the address of an exported function and whether it is Thumb.
func (im *Image) ExportVA(name string) (va uint32, thumb, ok bool) {
	v, ok := im.exports[name]
	return v &^ 1, v&1 != 0, ok
}

// PLTStub returns the ARM PLT entry that imports name.
func (im *Image) PLTStub(name string) (uint32, bool) {
	va, ok := im.plt[name]
	return va, ok
}

// indexPLT recovers import names from GNU ld's 12-byte ARM PLT entries by
// matching each computed GOT slot to its .rel.plt entry.
func (im *Image) indexPLT(dynsym []elf.Symbol) error {
	plt, rel := im.f.Section(".plt"), im.f.Section(".rel.plt")
	if plt == nil || rel == nil {
		return nil
	}
	code, err := plt.Data()
	if err != nil {
		return err
	}
	relocs, err := rel.Data()
	if err != nil {
		return err
	}
	le := binary.LittleEndian
	slots := map[uint32]string{}
	for i := 0; i+8 <= len(relocs); i += 8 {
		if sym := le.Uint32(relocs[i+4:]) >> 8; sym >= 1 && int(sym) <= len(dynsym) {
			slots[le.Uint32(relocs[i:])] = dynsym[sym-1].Name
		}
	}
	const plt0Size = 20
	for i := plt0Size; i+12 <= len(code); {
		w0, w1, w2 := le.Uint32(code[i:]), le.Uint32(code[i+4:]), le.Uint32(code[i+8:])
		if w0&0xFFFFF000 != 0xE28FC000 || w1&0xFFFFF000 != 0xE28CC000 || w2&0xFFDFF000 != 0xE59CF000 {
			i += 4 // includes the "bx pc; nop" Thumb shim preceding some entries
			continue
		}
		va := uint32(plt.Addr) + uint32(i)
		got := va + 8 + armImm(w0) + armImm(w1) + w2&0xFFF
		if name, ok := slots[got]; ok {
			if _, dup := im.plt[name]; !dup {
				im.plt[name] = va
			}
		}
		i += 12
	}
	return nil
}

// armImm decodes an ARM modified-immediate operand.
func armImm(w uint32) uint32 {
	rot := (w >> 8 & 0xF) * 2
	return w&0xFF>>rot | w&0xFF<<(32-rot)
}

// CString returns the printable NUL-terminated string at va inside read-only
// data. Strings under 4 bytes or over 512 are ignored as likely noise.
func (im *Image) CString(va uint32) (string, bool) {
	i := slices.IndexFunc(im.rodata, func(r addrRange) bool { return r.contains(va) })
	if i < 0 {
		return "", false
	}
	b := im.ReadVA(va, int(min(im.rodata[i].end-va, 512)))
	end := bytes.IndexByte(b, 0)
	if end < 4 {
		return "", false
	}
	for _, c := range b[:end] {
		if c < 9 || (c > 13 && c < 32 && c != 0x1B) || c >= 127 {
			return "", false
		}
	}
	return string(b[:end]), true
}

// FindString returns the address of every whole NUL-terminated occurrence of s
// in non-writable segments, including strings pooled in executable ones.
func (im *Image) FindString(s string) []uint32 {
	needle := append([]byte(s), 0)
	var out []uint32
	for _, seg := range im.segs {
		if seg.write || seg.filesz == 0 {
			continue
		}
		data := im.data[seg.off:seg.fileEnd()]
		for i := 0; ; {
			j := bytes.Index(data[i:], needle)
			if j < 0 {
				break
			}
			at := i + j
			if at == 0 || data[at-1] == 0 {
				out = append(out, seg.vaddr+uint32(at))
			}
			i = at + 1
		}
	}
	return out
}
