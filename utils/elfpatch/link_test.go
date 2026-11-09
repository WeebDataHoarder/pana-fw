package elfpatch

import (
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"
)

// TestLinkGolden links the embedded reference stub and compares byte-for-byte
// with the output of ld.lld for the same placement. testdata/hook.o is a
// freestanding Thumb stub (getenv + a tail call + a string literal); the golden
// is ld.lld's .text+.rodata for base 0x2a3487fc with the same symbol addresses.
func TestLinkGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/hook.o")
	if err != nil {
		t.Fatal(err)
	}
	const (
		base    = 0x2a3487fc
		getenv  = 0x2a344fec // ARM PLT stub
		origTl  = 0x2a37c69b // Thumb
		goldenH = "10b5044607487844fcf7f2eb20b10078592804bf002010bd2046bde8104033f03ebf00bf1e000000524547494f4e5f4652454500"
	)
	obj, err := LoadObject(data, "hook")
	if err != nil {
		t.Fatal(err)
	}
	externals := map[string]uint32{"getenv": getenv, "orig_tail": origTl}
	code, entry, err := obj.Link(base, func(n string) (uint32, bool) { v, ok := externals[n]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(code); got != goldenH {
		t.Errorf("linked output mismatch:\n got %s\nwant %s", got, goldenH)
	}
	if entry != base|1 {
		t.Errorf("entry = %#x, want %#x", entry, uint32(base|1))
	}
}

func TestLinkUnresolved(t *testing.T) {
	data, err := os.ReadFile("testdata/hook.o")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := LoadObject(data, "hook")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := obj.Link(0x2a3487fc, func(string) (uint32, bool) { return 0, false }); err == nil {
		t.Error("expected unresolved-external error")
	}
}

func TestApplyRelocErrors(t *testing.T) {
	bw := []byte{0xFF, 0xF7, 0xFE, 0xBF} // b.w . (addend -4)
	if err := applyReloc(bw, elf.R_ARM_THM_JUMP24, 0x30000, 0x10000, false); err == nil {
		t.Error("tail call to an ARM target must fail")
	}
	if err := applyReloc(bw, elf.R_ARM_THM_PC22, 0x30002, 0x10000, false); err == nil {
		t.Error("BLX to a misaligned ARM target must fail")
	}
}

func TestMovwAddend(t *testing.T) {
	field := []byte{0x40, 0xF2, 0x08, 0x00} // movw r0, #8 (in-place addend)
	if err := applyReloc(field, elf.R_ARM_THM_MOVW_ABS_NC, 0x12340, 0, true); err != nil {
		t.Fatal(err)
	}
	h1, h2 := binary.LittleEndian.Uint16(field), binary.LittleEndian.Uint16(field[2:])
	if got := thumbImm16(h1, h2); got != 0x2348 {
		t.Errorf("imm = %#x, want 0x2348", got)
	}
}

func TestSymbolValueKeepsOddDataAddress(t *testing.T) {
	o := &Object{
		syms:   []elf.Symbol{{Value: 0xB, Section: 1, Info: byte(elf.STT_OBJECT)}, {Value: 0x4, Section: 2, Info: byte(elf.STT_FUNC)}},
		placed: map[int]*placedSection{1: {offset: 0x10}, 2: {exec: true}},
	}
	if v, _, _ := o.symbolValue(1, 0x1000, nil); v != 0x101B {
		t.Errorf("data symbol = %#x, want 0x101b", v)
	}
	if v, thumb, _ := o.symbolValue(2, 0x1000, nil); v != 0x1004 || thumb {
		t.Errorf("ARM function = %#x thumb=%v, want 0x1004 and false", v, thumb)
	}
}
