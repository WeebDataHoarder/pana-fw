package elfpatch

import "slices"

// funcIndex holds the heuristic view of stripped code: function starts,
// literal-pool words, and what the code references.
type funcIndex struct {
	starts   []uint32
	literals map[uint32]struct{}
	strRefs  map[string][]uint32
	refs     map[uint32][]uint32 // address (bit 0 clear) -> instructions using it
}

// walk decodes [start,end) linearly, stepping over known literal words, until
// visit returns false.
func (im *Image) walk(start, end uint32, visit func(va uint32, k Insn) bool) {
	if end <= start {
		return
	}
	code := im.ReadVA(start, int(end-start))
	for i := uint32(0); int(i)+2 <= len(code); {
		va := start + i
		if _, lit := im.funcs.literals[va&^3]; lit {
			i = va&^3 + 4 - start
			continue
		}
		k, ok := Decode(code[i:], va)
		if !ok || !visit(va, k) {
			return
		}
		i += uint32(k.Len)
	}
}

func (im *Image) index() *funcIndex {
	if im.haveFuncs {
		return &im.funcs
	}
	im.haveFuncs = true
	im.funcs = funcIndex{literals: map[uint32]struct{}{}, strRefs: map[string][]uint32{}, refs: map[uint32][]uint32{}}

	seen := map[uint32]struct{}{}
	for _, v := range im.exports {
		if im.text.contains(v &^ 1) {
			seen[v&^1] = struct{}{}
		}
	}
	im.walk(im.text.start, im.text.end, func(va uint32, k Insn) bool {
		if k.H1&0xFF00 == 0xB500 || (k.H1 == 0xE92D && k.H2&0x4000 != 0) { // push {..., lr}
			seen[va] = struct{}{}
		}
		if k.Branch && k.Call && im.text.contains(k.Target) {
			seen[k.Target] = struct{}{}
		}
		if k.Literal {
			im.funcs.literals[k.LitAddr&^3] = struct{}{}
		}
		return true
	})
	im.funcs.starts = make([]uint32, 0, len(seen))
	for v := range seen {
		im.funcs.starts = append(im.funcs.starts, v)
	}
	slices.Sort(im.funcs.starts)
	im.indexRefs()
	return &im.funcs
}

// indexRefs tracks addresses materialised in registers within each function,
// and records them along with every immediate branch and adr target.
func (im *Image) indexRefs() {
	type ref struct {
		fn uint32
		s  string
	}
	recorded := map[ref]struct{}{}
	record := func(va, addr uint32) {
		if im.mapped(addr) {
			im.funcs.refs[addr&^1] = append(im.funcs.refs[addr&^1], va)
		}
		s, ok := im.CString(addr)
		if !ok {
			return
		}
		r := ref{im.FuncStart(va), s}
		if _, dup := recorded[r]; !dup {
			recorded[r] = struct{}{}
			im.funcs.strRefs[s] = append(im.funcs.strRefs[s], r.fn)
		}
	}

	var val [16]uint32
	var known uint16
	const callerSaved = 0x100F // r0-r3, ip
	next := 0
	im.walk(im.text.start, im.text.end, func(va uint32, k Insn) bool {
		for next < len(im.funcs.starts) && im.funcs.starts[next] <= va {
			known = 0
			next++
		}
		h, h2 := k.H1, k.H2
		if k.Branch && im.mapped(k.Target) {
			im.funcs.refs[k.Target&^1] = append(im.funcs.refs[k.Target&^1], va)
		}
		if k.Adr {
			record(va, k.AdrAddr)
		}
		switch {
		case k.Call:
			known &^= callerSaved
		case k.Literal:
			if w, ok := im.u32(k.LitAddr); ok {
				val[k.LitReg], known = w, known|1<<k.LitReg
				record(va, w)
			}
		case k.Len == 2 && h&0xFF78 == 0x4478: // add rd, pc
			if rd := int(h&7) | int(h>>4)&8; known&(1<<rd) != 0 {
				record(va, val[rd]+va+4)
				known &^= 1 << rd
			}
		case k.Len == 4 && h&0xFBF0 == 0xF240: // movw
			rd := int(h2>>8) & 0xF
			val[rd], known = thumbImm16(h, h2), known|1<<rd
		case k.Len == 4 && h&0xFBF0 == 0xF2C0: // movt
			rd := int(h2>>8) & 0xF
			low := val[rd] & 0xFFFF
			if known&(1<<rd) == 0 {
				low = 0
			}
			val[rd], known = low|thumbImm16(h, h2)<<16, known|1<<rd
			record(va, val[rd])
		}
		return true
	})
}

func thumbImm16(h, h2 uint16) uint32 {
	return uint32(h2&0xFF) | uint32(h2>>12&7)<<8 | uint32(h>>10&1)<<11 | uint32(h&0xF)<<12
}

func (im *Image) u32(va uint32) (uint32, bool) {
	b := im.ReadVA(va, 4)
	if b == nil {
		return 0, false
	}
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24, true
}

// FuncStart returns the start of the function containing va.
func (im *Image) FuncStart(va uint32) uint32 {
	starts := im.index().starts
	i, found := slices.BinarySearch(starts, va)
	if found {
		return va
	}
	if i == 0 {
		return im.text.start
	}
	return starts[i-1]
}

// FuncEnd returns the next detected function start after fn, or the text end.
// Functions without a detectable start make this an upper bound.
func (im *Image) FuncEnd(fn uint32) uint32 {
	starts := im.index().starts
	i, found := slices.BinarySearch(starts, fn)
	if found {
		i++
	}
	if i < len(starts) {
		return starts[i]
	}
	return im.text.end
}

// funcExtent returns a conservative end for the function at fn: the first
// terminator past every forward branch seen, extended over the literals the
// function loads, and never beyond FuncEnd.
func (im *Image) funcExtent(fn uint32) uint32 {
	limit := im.FuncEnd(fn)
	end, reach, pool := limit, fn, fn
	im.walk(fn, limit, func(va uint32, k Insn) bool {
		if k.Literal && k.LitAddr >= fn && k.LitAddr+4 <= limit {
			pool = max(pool, k.LitAddr+4)
		}
		if k.Branch && !k.Call && k.Target > va && k.Target < limit {
			reach = max(reach, k.Target)
		}
		next := va + uint32(k.Len)
		if k.Terminal && next > reach {
			end = max(next, pool)
			return false
		}
		return true
	})
	return end
}

func (im *Image) isFuncStart(va uint32) bool {
	_, found := slices.BinarySearch(im.index().starts, va)
	return found
}

// usedBy reports whether the instruction at user references the address, or
// with exact unset, whether any instruction in its function does.
func (im *Image) usedBy(va, user uint32, exact bool) bool {
	start := im.FuncStart(user)
	return slices.ContainsFunc(im.index().refs[va&^1], func(src uint32) bool {
		return src == user || (!exact && im.FuncStart(src) == start)
	})
}

// referenceInto returns the lowest instruction outside r that references r.
func (im *Image) referenceInto(r addrRange) (from uint32, found bool) {
	for addr, sources := range im.index().refs {
		if !r.contains(addr) {
			continue
		}
		for _, src := range sources {
			if !r.contains(src) && (!found || src < from) {
				from, found = src, true
			}
		}
	}
	return from, found
}

// pointerInto returns the first aligned word outside r that holds an address
// in r, including tables embedded in code.
func (im *Image) pointerInto(r addrRange) (at uint32, found bool) {
	for _, s := range im.segs {
		for va := alignUp(s.vaddr, 4); uint64(va)+4 <= uint64(s.vaEnd()); va += 4 {
			if r.contains(va) {
				continue
			}
			if w, ok := im.u32(va); ok && r.contains(w&^1) {
				return va, true
			}
		}
	}
	return 0, false
}

// isPadding reports whether va holds a filler halfword: zero, nop or mov r8, r8.
func (im *Image) isPadding(va uint32) bool {
	b := im.ReadVA(va, 2)
	if b == nil {
		return false
	}
	h := uint16(b[0]) | uint16(b[1])<<8
	return h == 0 || h == 0xBF00 || h == 0x46C0
}

func (im *Image) isLiteral(va uint32) bool {
	_, ok := im.index().literals[va&^3]
	return ok
}

// FuncsByString returns the functions referencing the exact string s.
func (im *Image) FuncsByString(s string) []uint32 { return im.index().strRefs[s] }

// insideIT reports whether va is a conditional instruction of an IT block.
func (im *Image) insideIT(va uint32) bool {
	fn := im.FuncStart(va)
	remaining, inside := 0, false
	im.walk(fn, im.FuncEnd(fn), func(at uint32, k Insn) bool {
		if at >= va {
			inside = at == va && remaining > 0
			return false
		}
		remaining = max(remaining-1, k.ITCount)
		return true
	})
	return inside
}

// branchesInto reports whether any branch in the function containing site
// targets the open range (site, end).
func (im *Image) branchesInto(site, end uint32) (from uint32, found bool) {
	fn := im.FuncStart(site)
	im.walk(fn, im.FuncEnd(fn), func(va uint32, k Insn) bool {
		if k.Branch && !k.Call && k.Target > site && k.Target < end {
			from, found = va, true
			return false
		}
		return true
	})
	return from, found
}
