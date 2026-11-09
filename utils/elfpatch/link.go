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
	maxObjectAlign = 1 << 12
	maxObjectSize  = 1 << 24
)

// Object is a compiled Thumb stub (ELF32 ARM relocatable) reduced to the
// sections reachable from its entry symbol.
type Object struct {
	Size, Align uint32

	syms     []elf.Symbol
	placed   map[int]*placedSection
	order    []*placedSection
	entryOff uint32
}

type placedSection struct {
	offset uint32
	data   []byte
	exec   bool
	relocs []relocation
}

type relocation struct {
	offset uint32
	typ    elf.R_ARM
	sym    uint32
}

// LoadObject parses a stub object. An empty entry selects the symbol "hook", or
// the object's only global function.
func LoadObject(data []byte, entry string) (*Object, error) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if f.Class != elf.ELFCLASS32 || f.ByteOrder != binary.LittleEndian || f.Machine != elf.EM_ARM || f.Type != elf.ET_REL {
		return nil, errors.New("stub must be a little-endian ELF32 ARM relocatable object")
	}
	syms, err := f.Symbols()
	if err != nil {
		return nil, err
	}
	relocs, err := readRelocs(f)
	if err != nil {
		return nil, err
	}
	sym, err := selectEntry(syms, entry)
	if err != nil {
		return nil, err
	}
	if sym.Section >= elf.SectionIndex(len(f.Sections)) || !isPlaceable(f.Sections[sym.Section]) {
		return nil, fmt.Errorf("entry %q is not in a placeable section", sym.Name)
	}
	if elf.ST_TYPE(sym.Info) == elf.STT_FUNC && sym.Value&1 == 0 {
		return nil, fmt.Errorf("entry %q is not Thumb code", sym.Name)
	}

	o := &Object{syms: syms, placed: map[int]*placedSection{}, Align: 1}
	entrySec := int(sym.Section)
	idx := reachableSections(f, syms, relocs, entrySec)
	slices.SortFunc(idx, func(a, b int) int { return sectionRank(f, a, entrySec) - sectionRank(f, b, entrySec) })
	for _, i := range idx {
		s := f.Sections[i]
		if s.Flags&elf.SHF_WRITE != 0 {
			return nil, fmt.Errorf("section %s is writable; stubs are placed in read-only code", s.Name)
		}
		align := max(uint32(s.Addralign), 1)
		if align&(align-1) != 0 || align > maxObjectAlign {
			return nil, fmt.Errorf("section %s has invalid alignment %d", s.Name, align)
		}
		content, err := s.Data()
		if err != nil {
			return nil, err
		}
		for _, r := range relocs[i] {
			if uint64(r.offset)+4 > uint64(len(content)) {
				return nil, fmt.Errorf("relocation at %#x outside section %s", r.offset, s.Name)
			}
		}
		o.Align = max(o.Align, align)
		o.Size = alignUp(o.Size, align)
		p := &placedSection{offset: o.Size, data: content, exec: s.Flags&elf.SHF_EXECINSTR != 0, relocs: relocs[i]}
		o.placed[i], o.order = p, append(o.order, p)
		o.Size += uint32(len(content))
		if o.Size > maxObjectSize {
			return nil, errors.New("stub is larger than any branch can reach")
		}
	}
	o.entryOff = o.placed[entrySec].offset + uint32(sym.Value)&^1
	return o, nil
}

func isPlaceable(s *elf.Section) bool {
	return s.Type == elf.SHT_PROGBITS && s.Flags&elf.SHF_ALLOC != 0
}

func readRelocs(f *elf.File) (map[int][]relocation, error) {
	out := map[int][]relocation{}
	for _, s := range f.Sections {
		switch s.Type {
		case elf.SHT_RELA:
			return nil, fmt.Errorf("section %s: RELA relocations are not supported", s.Name)
		case elf.SHT_REL:
			d, err := s.Data()
			if err != nil {
				return nil, err
			}
			for i := 0; i+8 <= len(d); i += 8 {
				info := binary.LittleEndian.Uint32(d[i+4:])
				out[int(s.Info)] = append(out[int(s.Info)], relocation{
					binary.LittleEndian.Uint32(d[i:]), elf.R_ARM(info & 0xFF), info >> 8,
				})
			}
		}
	}
	return out, nil
}

func selectEntry(syms []elf.Symbol, entry string) (elf.Symbol, error) {
	var hook, only []elf.Symbol
	for _, s := range syms {
		if s.Section == elf.SHN_UNDEF || s.Name == "" {
			continue
		}
		if entry != "" && s.Name == entry {
			return s, nil
		}
		if s.Name == "hook" {
			hook = append(hook, s)
		}
		if elf.ST_BIND(s.Info) == elf.STB_GLOBAL && elf.ST_TYPE(s.Info) == elf.STT_FUNC {
			only = append(only, s)
		}
	}
	if entry != "" {
		return elf.Symbol{}, fmt.Errorf("entry symbol %q not defined by stub", entry)
	}
	if len(hook) > 0 {
		return hook[0], nil
	}
	return single("default entry (a sole global function; otherwise set edit.entry)", only)
}

// reachableSections returns the placeable sections the entry section needs.
func reachableSections(f *elf.File, syms []elf.Symbol, relocs map[int][]relocation, root int) []int {
	seen := map[int]struct{}{root: {}}
	queue := []int{root}
	for len(queue) > 0 {
		sec := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, r := range relocs[sec] {
			if r.sym == 0 || int(r.sym) > len(syms) {
				continue
			}
			target := int(syms[r.sym-1].Section)
			if _, dup := seen[target]; dup || target <= 0 || target >= len(f.Sections) || !isPlaceable(f.Sections[target]) {
				continue
			}
			seen[target] = struct{}{}
			queue = append(queue, target)
		}
	}
	out := make([]int, 0, len(seen))
	for i := range seen {
		out = append(out, i)
	}
	return out
}

// sectionRank orders the entry section first, then code, then data, stably by index.
func sectionRank(f *elf.File, i, entry int) int {
	switch {
	case i == entry:
		return -1
	case f.Sections[i].Flags&elf.SHF_EXECINSTR != 0:
		return i
	default:
		return i + len(f.Sections)
	}
}

// Link returns the object's bytes for placement at base, which must satisfy
// Align, and its entry address with the Thumb bit set. resolve supplies
// undefined symbols; bit 0 of a code address selects Thumb.
func (o *Object) Link(base uint32, resolve func(string) (uint32, bool)) ([]byte, uint32, error) {
	if base%o.Align != 0 {
		return nil, 0, fmt.Errorf("base %#x is not aligned to %d", base, o.Align)
	}
	img := make([]byte, o.Size)
	for _, sec := range o.order {
		copy(img[sec.offset:], sec.data)
	}
	for _, sec := range o.order {
		for _, r := range sec.relocs {
			S, thumbCode, err := o.symbolValue(r.sym, base, resolve)
			if err != nil {
				return nil, 0, fmt.Errorf("%s: %w", r.typ, err)
			}
			if r.typ == elf.R_ARM_THM_PC22 && o.syms[r.sym-1].Name == "_ret" {
				return nil, 0, errors.New("_ret must be tail-called, not called: the host code never returns to the stub")
			}
			at := sec.offset + r.offset
			if err := applyReloc(img[at:at+4], r.typ, S, base+at, thumbCode); err != nil {
				return nil, 0, err
			}
		}
	}
	return img, (base + o.entryOff) | 1, nil
}

// symbolValue returns the symbol's address, with bit 0 set for Thumb functions,
// and whether a branch to it stays in Thumb state regardless of that bit.
func (o *Object) symbolValue(index, base uint32, resolve func(string) (uint32, bool)) (S uint32, thumbCode bool, err error) {
	if index == 0 || int(index) > len(o.syms) {
		return 0, false, fmt.Errorf("bad symbol index %d", index)
	}
	s := o.syms[index-1]
	if s.Section == elf.SHN_UNDEF {
		v, ok := resolve(s.Name)
		if !ok {
			return 0, false, fmt.Errorf("unresolved external %q", s.Name)
		}
		return v, false, nil
	}
	sec, ok := o.placed[int(s.Section)]
	if !ok {
		return 0, false, fmt.Errorf("symbol %q is in an unplaced section", s.Name)
	}
	armFunc := elf.ST_TYPE(s.Info) == elf.STT_FUNC && s.Value&1 == 0
	return base + sec.offset + uint32(s.Value), sec.exec && !armFunc, nil
}

// applyReloc patches the 4 bytes in field. Bit 0 of S or thumbCode selects Thumb
// state for branches. Addends are in place.
func applyReloc(field []byte, rt elf.R_ARM, S, P uint32, thumbCode bool) error {
	le := binary.LittleEndian
	switch rt {
	case elf.R_ARM_THM_JUMP24, elf.R_ARM_THM_PC22:
		h1, h2 := le.Uint16(field), le.Uint16(field[2:])
		target := int64(S&^1) + int64(branchDisp(h1, h2))
		thumb := thumbCode || S&1 != 0
		top, from := uint16(topBW), P
		switch {
		case rt == elf.R_ARM_THM_JUMP24 && !thumb:
			return fmt.Errorf("tail call at %#x cannot reach ARM target %#x; add a non-tail call", P, S)
		case rt == elf.R_ARM_THM_PC22 && thumb:
			top = topBL
		case rt == elf.R_ARM_THM_PC22:
			if S&3 != 0 {
				return fmt.Errorf("BLX at %#x to misaligned ARM target %#x", P, S)
			}
			top, from = topBLX, P&^3
		}
		diff := target - int64(from)
		if !branchInRange(diff) {
			return fmt.Errorf("%s target %#x out of range from %#x", rt, S, P)
		}
		h1, h2 = encodeBranchBits(uint32(diff), top)
		le.PutUint16(field, h1)
		le.PutUint16(field[2:], h2)

	case elf.R_ARM_THM_MOVW_ABS_NC, elf.R_ARM_THM_MOVT_ABS:
		h1, h2 := le.Uint16(field), le.Uint16(field[2:])
		v := S + uint32(int32(int16(thumbImm16(h1, h2))))
		if rt == elf.R_ARM_THM_MOVT_ABS {
			v >>= 16
		}
		le.PutUint16(field, h1&0xFBF0|uint16(v>>12&0xF)|uint16(v>>11&1)<<10)
		le.PutUint16(field[2:], h2&0x8F00|uint16(v>>8&7)<<12|uint16(v&0xFF))

	case elf.R_ARM_ABS32:
		le.PutUint32(field, le.Uint32(field)+S)

	case elf.R_ARM_REL32:
		le.PutUint32(field, le.Uint32(field)+S-P)

	default:
		return fmt.Errorf("unhandled relocation %s", rt)
	}
	return nil
}

func alignUp(v, a uint32) uint32 {
	if a <= 1 {
		return v
	}
	return (v + a - 1) &^ (a - 1)
}
