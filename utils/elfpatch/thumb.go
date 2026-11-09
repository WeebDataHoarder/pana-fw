package elfpatch

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

// InsnLen returns the byte length of the Thumb instruction starting with h.
func InsnLen(h uint16) int {
	switch h >> 11 {
	case 0b11101, 0b11110, 0b11111:
		return 4
	default:
		return 2
	}
}

// Insn classifies one Thumb instruction. Only the properties needed for
// anchoring and detour safety are decoded.
type Insn struct {
	H1, H2 uint16
	Len    int

	ITCount int  // instructions conditioned by an IT, 0 for anything else
	PCRel   bool // reads PC, so it cannot execute from another address
	Branch  bool // immediate branch or call to Target
	Call    bool
	// Terminal means control does not fall through to the next instruction.
	Terminal bool
	Target   uint32

	Literal bool // 32-bit load from LitAddr into LitReg
	LitReg  int
	LitAddr uint32

	Adr     bool // adr, materialising AdrAddr
	AdrAddr uint32
}

// Decode classifies the instruction at code[0:], located at pc. It reports
// false when code is too short to hold the instruction.
func Decode(code []byte, pc uint32) (k Insn, ok bool) {
	if len(code) < 2 {
		return k, false
	}
	k.H1 = binary.LittleEndian.Uint16(code)
	k.Len = InsnLen(k.H1)
	if len(code) < k.Len {
		return k, false
	}
	if k.Len == 2 {
		k.decode16(pc)
	} else {
		k.H2 = binary.LittleEndian.Uint16(code[2:])
		k.decode32(pc)
	}
	return k, true
}

func (k *Insn) literal(reg int, addr uint32) {
	k.PCRel, k.Literal, k.LitReg, k.LitAddr = true, true, reg, addr
}

func (k *Insn) decode16(pc uint32) {
	h := k.H1
	switch {
	case h&0xFF00 == 0xBF00 && h&0xF != 0: // IT
		k.ITCount = 4 - bits.TrailingZeros8(uint8(h&0xF))
	case h&0xF800 == 0x4800: // ldr rt, [pc, #imm]
		k.literal(int(h>>8)&7, (pc+4)&^3+uint32(h&0xFF)*4)
	case h&0xF800 == 0xA000: // adr
		k.PCRel, k.Adr, k.AdrAddr = true, true, (pc+4)&^3+uint32(h&0xFF)*4
	case h&0xFC00 == 0x4400: // add, cmp and mov with high registers; bx, blx
		k.decodeHighReg()
	case h&0xF000 == 0xD000 && h&0xE00 != 0xE00: // b<c>
		k.Branch = true
		k.Target = pc + 4 + uint32(int32(int8(h))<<1)
	case h&0xF800 == 0xE000: // b
		k.Branch, k.Terminal = true, true
		k.Target = pc + 4 + uint32(int32(h&0x7FF)<<21>>20)
	case h&0xF500 == 0xB100: // cbz, cbnz
		k.Branch = true
		k.Target = pc + 4 + uint32(h>>9&1)<<6 + uint32(h>>3&0x1F)<<1
	case h&0xFF00 == 0xBD00: // pop {..., pc}
		k.Terminal = true
	}
}

// decodeHighReg classifies the 16-bit forms that can name pc directly.
func (k *Insn) decodeHighReg() {
	h := k.H1
	op, rm, rd := h>>8&3, h>>3&0xF, h&7|h>>4&8
	if op == 3 { // bx, blx
		k.PCRel = rm == 15
		k.Call = h&0x80 != 0
		k.Terminal = !k.Call
		return
	}
	k.PCRel = rm == 15 || (op != 2 && rd == 15) // mov does not read rd
	k.Terminal = op != 1 && rd == 15            // cmp does not write rd
}

func (k *Insn) decode32(pc uint32) {
	h, h2 := k.H1, k.H2
	isBranch := h&0xF800 == 0xF000
	switch {
	case isBranch && h2&0xD000 == 0xD000: // bl
		k.Branch, k.Call = true, true
		k.Target = pc + 4 + uint32(branchDisp(h, h2))
	case isBranch && h2&0xD001 == 0xC000: // blx
		k.Branch, k.Call = true, true
		k.Target = (pc+4)&^3 + uint32(branchDisp(h, h2))
	case isBranch && h2&0xD000 == 0x9000: // b.w
		k.Branch, k.Terminal = true, true
		k.Target = pc + 4 + uint32(branchDisp(h, h2))
	case isBranch && h2&0xD000 == 0x8000 && h>>6&0xF < 0xE: // b<c>.w
		k.Branch = true
		k.Target = pc + 4 + uint32(condBranchDisp(h, h2))
	case h&0xFF7F == 0xF85F: // ldr.w rt, [pc, #imm]
		addr := (pc + 4) &^ 3
		if h&0x80 != 0 {
			addr += uint32(h2 & 0xFFF)
		} else {
			addr -= uint32(h2 & 0xFFF)
		}
		k.literal(int(h2>>12), addr)
		k.Terminal = h2>>12 == 15
	case h&0xFF7F == 0xF81F, h&0xFF7F == 0xF83F, h&0xFF7F == 0xF91F, h&0xFF7F == 0xF93F, // ldrb/h/sb/sh literal
		h&0xFF7F == 0xE95F, // ldrd literal
		h&0xFF3F == 0xED1F: // vldr literal
		k.PCRel = true
	case h&0xFBFF == 0xF20F, h&0xFBFF == 0xF2AF: // adr.w
		imm := uint32(h>>10&1)<<11 | uint32(h2>>12&7)<<8 | uint32(h2&0xFF)
		if h&0xFBFF == 0xF2AF {
			imm = -imm
		}
		k.PCRel, k.Adr, k.AdrAddr = true, true, (pc+4)&^3+imm
	case h&0xFFF0 == 0xE8D0 && h2&0xFFE0 == 0xF000: // tbb, tbh
		k.PCRel = h&0xF == 0xF
		k.Terminal = true
	case h == 0xE8BD && h2&0x8000 != 0, // pop.w {..., pc}
		h == 0xF85D && h2&0xF000 == 0xF000: // ldr pc, [sp], #imm
		k.Terminal = true
	}
}

// branchDisp returns the signed displacement of a BL, BLX or B.W.
func branchDisp(h1, h2 uint16) int32 {
	s := uint32(h1>>10) & 1
	i1 := ^(uint32(h2>>13) ^ s) & 1
	i2 := ^(uint32(h2>>11) ^ s) & 1
	off := s<<24 | i1<<23 | i2<<22 | uint32(h1&0x3FF)<<12 | uint32(h2&0x7FF)<<1
	return int32(off<<7) >> 7
}

func condBranchDisp(h1, h2 uint16) int32 {
	off := uint32(h1>>10&1)<<20 | uint32(h2>>11&1)<<19 | uint32(h2>>13&1)<<18 |
		uint32(h1&0x3F)<<12 | uint32(h2&0x7FF)<<1
	return int32(off<<11) >> 11
}

// High halfword templates for the 32-bit branch encodings.
const (
	topBW  = 0x9000
	topBLX = 0xC000
	topBL  = 0xD000
)

// EncodeBW encodes a B.W at pc. It reports false for an odd or out-of-range target.
func EncodeBW(pc, target uint32) (h1, h2 uint16, ok bool) {
	return encodeBranch(pc, target, topBW)
}

// EncodeBL encodes a BL at pc. It reports false for an odd or out-of-range target.
func EncodeBL(pc, target uint32) (h1, h2 uint16, ok bool) {
	return encodeBranch(pc, target, topBL)
}

func encodeBranch(pc, target uint32, top uint16) (h1, h2 uint16, ok bool) {
	diff := int64(target) - int64(pc+4)
	if !branchInRange(diff) || diff&1 != 0 {
		return 0, 0, false
	}
	h1, h2 = encodeBranchBits(uint32(diff), top)
	return h1, h2, true
}

func branchInRange(diff int64) bool { return diff >= -(1<<24) && diff < 1<<24 }

func encodeBranchBits(off uint32, top uint16) (h1, h2 uint16) {
	s := off >> 24 & 1
	j1 := ^(off>>23 ^ s) & 1
	j2 := ^(off>>22 ^ s) & 1
	h1 = uint16(0xF000 | s<<10 | off>>12&0x3FF)
	h2 = uint16(uint32(top) | j1<<13 | j2<<11 | off>>1&0x7FF)
	return h1, h2
}

// thumbAsm builds position-independent glue, whose layout does not depend on
// its address once aligned, so it can be sized before it is placed.
type thumbAsm struct {
	code    []byte
	fixups  []asmFixup
	hasPool bool
}

// align returns the placement alignment the code requires.
func (a *thumbAsm) align() uint32 {
	if a.hasPool {
		return 4
	}
	return 2
}

type asmFixup struct {
	off    int
	target uint32
	top    uint16
}

func (a *thumbAsm) op(halfwords ...uint16) {
	for _, h := range halfwords {
		a.code = binary.LittleEndian.AppendUint16(a.code, h)
	}
}

// branch emits a 32-bit branch to an absolute Thumb address, resolved by link.
func (a *thumbAsm) branch(target uint32, top uint16) {
	a.fixups = append(a.fixups, asmFixup{len(a.code), target &^ 1, top})
	a.op(0, 0)
}

// pool aligns the code, appends words, and patches the imm8 of the PC-relative
// instruction at loadOff to address the first of them.
func (a *thumbAsm) pool(loadOff int, words []uint32) {
	a.hasPool = true
	for len(a.code)%4 != 0 {
		a.op(0xBF00)
	}
	imm := (len(a.code) - (loadOff+4)&^3) / 4
	binary.LittleEndian.PutUint16(a.code[loadOff:], binary.LittleEndian.Uint16(a.code[loadOff:])|uint16(imm))
	for _, w := range words {
		a.code = binary.LittleEndian.AppendUint32(a.code, w)
	}
}

// link resolves the branches for placement at va, which must satisfy align.
func (a *thumbAsm) link(va uint32) ([]byte, error) {
	for _, f := range a.fixups {
		pc := va + uint32(f.off)
		h1, h2, ok := encodeBranch(pc, f.target, f.top)
		if !ok {
			return nil, fmt.Errorf("glue branch %#x -> %#x out of range", pc, f.target)
		}
		binary.LittleEndian.PutUint16(a.code[f.off:], h1)
		binary.LittleEndian.PutUint16(a.code[f.off+2:], h2)
	}
	return a.code, nil
}
