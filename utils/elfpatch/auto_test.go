package elfpatch

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

const (
	autoBase   = 0x10000
	autoDataVA = 0x30000
	autoCode   = 0x100 // file offset of the patched function
)

// autoELF builds an executable with no padding after its code: the RW segment
// starts in the file right after it, followed by a non-loaded section and the
// section headers.
func autoELF(t *testing.T, textSize, dataVA uint32) []byte {
	t.Helper()
	le := binary.LittleEndian
	const ehsize, phentsize, shentsize = 52, 32, 40
	dataOff := textSize
	dataSize := uint32(0x40)
	commentOff := dataOff + dataSize
	shoff := commentOff + 0x10
	b := make([]byte, shoff+4*shentsize)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 1, 1, 1, 0})
	le.PutUint16(b[16:], 2)
	le.PutUint16(b[18:], 40)
	le.PutUint32(b[20:], 1)
	le.PutUint32(b[24:], autoBase+autoCode)
	le.PutUint32(b[28:], ehsize)
	le.PutUint32(b[32:], shoff)
	le.PutUint16(b[40:], ehsize)
	le.PutUint16(b[42:], phentsize)
	le.PutUint16(b[44:], 3)
	le.PutUint16(b[46:], shentsize)
	le.PutUint16(b[48:], 4)
	le.PutUint16(b[50:], 3)
	ph := func(i int, typ, off, va, filesz, memsz, flags, align uint32) {
		p := b[ehsize+i*phentsize:]
		for j, v := range []uint32{typ, off, va, va, filesz, memsz, flags, align} {
			le.PutUint32(p[j*4:], v)
		}
	}
	ph(0, 1, 0, autoBase, textSize, textSize, 5, 0x1000)
	ph(1, 1, dataOff, dataVA, dataSize, dataSize+0x100, 6, 0x1000)
	ph(2, 2, dataOff+8, dataVA+8, 8, 8, 6, 4) // PT_DYNAMIC inside the data segment
	sh := func(i int, typ, flags, addr, off, size uint32) {
		s := b[shoff+uint32(i)*shentsize:]
		for j, v := range []uint32{0, typ, flags, addr, off, size} {
			le.PutUint32(s[j*4:], v)
		}
	}
	strs := commentOff + 8
	sh(1, uint32(elf.SHT_PROGBITS), uint32(elf.SHF_ALLOC|elf.SHF_WRITE), dataVA, dataOff, dataSize)
	sh(2, uint32(elf.SHT_PROGBITS), 0, 0, commentOff, 8)
	sh(3, uint32(elf.SHT_STRTAB), 0, 0, strs, 1)
	copy(b[dataOff:], "DATA-MARKER")
	copy(b[commentOff:], "COMMENT!")
	copy(b[autoCode:], []byte{0x70, 0xB5, 0x00, 0xBF, 0x00, 0xBF, 0x00, 0xBD})
	copy(b[0x200:], fnBytes) // external target
	b[textSize-1] = 0xFF     // no padding to claim
	return b
}

func autoPatch(va uint32, caves ...CaveSpec) *PatchSet {
	return &PatchSet{
		Objects: map[string]string{"h": "h"},
		Caves:   caves,
		Edits: []Edit{{
			Name: "d", Action: "detour", Object: "h", Entry: "hook",
			Anchor: Anchor{VA: &va, Expect: "70 b5 00 bf 00 bf 00 bd"},
			Externals: map[string]ExtRef{
				"getenv":    {VA: u32p(autoBase + 0x200 + 1), Expect: fnExpect},
				"orig_tail": {VA: u32p(autoBase + 0x200 + 1), Expect: fnExpect},
			},
		}},
	}
}

func openHook(string) ([]byte, error) { return os.ReadFile("testdata/hook.o") }

func auto() CaveSpec { return CaveSpec{Auto: &AutoSpec{}} }

func TestAutoGrowsSegment(t *testing.T) {
	const text = 0x1000
	in := autoELF(t, text, autoDataVA)
	out, err := autoPatch(autoBase+autoCode, auto()).Apply(in, openHook)
	if err != nil {
		t.Fatal(err)
	}
	if want := len(in) + 0x1000; len(out) != want {
		t.Fatalf("size = %#x, want %#x", len(out), want)
	}
	f, err := elf.NewFile(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	rx, rw, dyn := f.Progs[0], f.Progs[1], f.Progs[2]
	if rx.Vaddr != autoBase || rx.Filesz != text+0x1000 || rx.Memsz != rx.Filesz {
		t.Errorf("code segment = %+v", rx.ProgHeader)
	}
	if rw.Vaddr != autoDataVA || rw.Off != text+0x1000 || rw.Filesz != 0x40 || rw.Memsz != 0x140 {
		t.Errorf("data segment = %+v", rw.ProgHeader)
	}
	if dyn.Off != rw.Off+8 {
		t.Errorf("PT_DYNAMIC offset = %#x, want %#x", dyn.Off, rw.Off+8)
	}
	if !bytes.HasPrefix(out[rw.Off:], []byte("DATA-MARKER")) {
		t.Error("data segment contents moved incorrectly")
	}
	sec := f.Sections[2]
	if d, err := sec.Data(); err != nil || string(d) != "COMMENT!" {
		t.Errorf("non-loaded section = %q, %v", d, err)
	}

	im := mustOpen(t, out)
	k, ok := Decode(im.ReadVA(autoBase+autoCode, 4), autoBase+autoCode)
	if !ok || !k.Branch || k.Target < autoBase+text || k.Target >= autoBase+text+0x1000 {
		t.Errorf("site target = %#x, want the grown span", k.Target)
	}
}

func TestAutoKeepsAlignmentCongruent(t *testing.T) {
	in := autoELF(t, 0x1000, autoDataVA)
	binary.LittleEndian.PutUint32(in[52+32+28:], 0x10000)          // data p_align
	binary.LittleEndian.PutUint32(in[52+32+8:], autoDataVA+0x1000) // keeps vaddr-offset a multiple of it
	out, err := autoPatch(autoBase+autoCode, auto()).Apply(in, openHook)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(out) - len(in); got != 0x10000 {
		t.Errorf("inserted %#x bytes, want a whole p_align", got)
	}
	f, err := elf.NewFile(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if rw := f.Progs[1]; (rw.Vaddr-rw.Off)%0x10000 != 0 {
		t.Errorf("offset %#x not congruent with %#x", rw.Off, rw.Vaddr)
	}
}

func TestAutoPrefersDeclaredCaves(t *testing.T) {
	in := autoELF(t, 0x1000, autoDataVA)
	copy(in[0x400:], fnBytes)
	copy(in[0x480:], fnBytes)
	cave := CaveSpec{Range: &VARange{Start: autoBase + 0x400, End: autoBase + 0x480, Expect: fnExpect}}
	out, err := autoPatch(autoBase+autoCode, cave, auto()).Apply(in, openHook)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Errorf("grew by %#x bytes despite a sufficient declared cave", len(out)-len(in))
	}
}

func TestAutoPrefersPadding(t *testing.T) {
	in := autoELF(t, 0x1000, autoDataVA)
	binary.LittleEndian.PutUint32(in[52+16:], 0xE00) // code ends with padding before the data
	binary.LittleEndian.PutUint32(in[52+20:], 0xE00)
	in[0xFFF] = 0
	out, err := autoPatch(autoBase+autoCode, auto()).Apply(in, openHook)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Errorf("grew by %#x bytes despite sufficient padding", len(out)-len(in))
	}
	if rx := mustOpen(t, out).firstExec(); rx.filesz != 0x1000 {
		t.Errorf("code filesz = %#x, want the padding claimed", rx.filesz)
	}
}

func TestAutoDisabledByDefault(t *testing.T) {
	applyErr(t, autoPatch(autoBase+autoCode), autoELF(t, 0x1000, autoDataVA))
}

func applyErrContains(t *testing.T, ps *PatchSet, data []byte, want string) {
	t.Helper()
	_, err := ps.Apply(data, openHook)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}
}

func TestAutoRefusals(t *testing.T) {
	patch := autoPatch(autoBase+autoCode, auto())
	applyErrContains(t, patch, autoELF(t, 0x1000, autoBase+0x1000), "can grow by 0 bytes")

	in := autoELF(t, 0x1000, autoDataVA)
	binary.LittleEndian.PutUint32(in[52+20:], 0x1800) // memsz > filesz
	applyErrContains(t, patch, in, "zero-initialised tail")

	in = autoELF(t, 0x1000, autoDataVA)
	shoff := binary.LittleEndian.Uint32(in[32:])
	binary.LittleEndian.PutUint32(in[shoff+40*2+16:], 0xFF0)
	binary.LittleEndian.PutUint32(in[shoff+40*2+20:], 0x20)
	applyErrContains(t, patch, in, "spans the end")

	im := mustOpen(t, autoELF(t, 0x1000, autoBase+0x2000))
	if _, _, err := im.ExtendExec(0x2000); err == nil {
		t.Error("grew past the next segment")
	}
}

func TestExtendExecHeaderTablesAndLimits(t *testing.T) {
	le := binary.LittleEndian
	// The code segment ends inside the program header table.
	in := autoELF(t, 0x1000, autoDataVA)
	le.PutUint32(in[52+16:], 100)
	le.PutUint32(in[52+20:], 100)
	if _, _, err := mustOpen(t, in).ExtendExec(0x10); err == nil || !strings.Contains(err.Error(), "program headers") {
		t.Errorf("program header straddle: %v", err)
	}
	// The code segment ends inside the section header table.
	in = autoELF(t, 0x1000, autoDataVA)
	shoff := le.Uint32(in[32:])
	le.PutUint32(in[52+16:], shoff+40)
	le.PutUint32(in[52+20:], shoff+40)
	if _, _, err := mustOpen(t, in).ExtendExec(0x10); err == nil || !strings.Contains(err.Error(), "spans the end") {
		t.Errorf("section header straddle: %v", err)
	}
	// An alignment too large to honour.
	in = autoELF(t, 0x1000, autoDataVA)
	le.PutUint32(in[52+32+28:], 0x40000000)
	if _, _, err := mustOpen(t, in).ExtendExec(0x10); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("huge alignment: %v", err)
	}
	// A section offset that cannot move.
	in = autoELF(t, 0x1000, autoDataVA)
	le.PutUint32(in[shoff+40*2+16:], 0xFFFFFFF0)
	if im, err := Open(in); err == nil {
		if _, _, err := im.ExtendExec(0x10); err == nil {
			t.Error("section offset wrapped")
		}
	}
}

func TestExtendExecStopsBeforeMidPageSegment(t *testing.T) {
	const text = 0x102C
	im := mustOpen(t, autoELF(t, text, autoBase+0x202C))
	start, end, err := im.ExtendExec(0x100)
	if err != nil {
		t.Fatal(err)
	}
	if start != autoBase+text || end != autoBase+0x2000 {
		t.Errorf("span = %#x..%#x, want it to stop at the next segment's page", start, end)
	}
}

func TestExtendExecRepeats(t *testing.T) {
	im := mustOpen(t, autoELF(t, 0x1000, autoDataVA))
	_, first, err := im.ExtendExec(0x1000)
	if err != nil {
		t.Fatal(err)
	}
	start, _, err := im.ExtendExec(0x1000)
	if err != nil {
		t.Fatal(err)
	}
	if start != first {
		t.Errorf("second span starts at %#x, want %#x", start, first)
	}
	if rw := mustOpen(t, im.Bytes()).segs[1]; rw.off != 0x3000 {
		t.Errorf("data offset = %#x, want 0x3000", rw.off)
	}
	if !bytes.HasPrefix(im.ReadVA(autoDataVA, 16), []byte("DATA-MARKER")) {
		t.Error("data lost after repeated growth")
	}
}

func TestExtendExecWithoutSectionTable(t *testing.T) {
	le := binary.LittleEndian
	in := autoELF(t, 0x1000, autoDataVA)
	le.PutUint32(in[32:], 0)
	le.PutUint16(in[48:], 0)
	le.PutUint16(in[50:], 0)
	im := mustOpen(t, in)
	if _, _, err := im.ExtendExec(0x10); err != nil {
		t.Fatal(err)
	}
	if got := le.Uint32(im.Bytes()[32:]); got != 0 {
		t.Errorf("e_shoff = %#x, want 0", got)
	}
}

func TestExtendExecIgnoresNOBITSExtent(t *testing.T) {
	le := binary.LittleEndian
	in := autoELF(t, 0x1000, autoDataVA)
	shoff := le.Uint32(in[32:])
	le.PutUint32(in[shoff+40*2+4:], uint32(elf.SHT_NOBITS))
	le.PutUint32(in[shoff+40*2+16:], 0xFF0)
	le.PutUint32(in[shoff+40*2+20:], 0x20)
	if _, _, err := mustOpen(t, in).ExtendExec(0x10); err != nil {
		t.Errorf("NOBITS section blocked growth: %v", err)
	}
}

func TestAutoMergesGrownSpace(t *testing.T) {
	im := mustOpen(t, autoELF(t, 0x1000, autoDataVA))
	a := NewArena(im)
	a.Auto()
	first, err := a.Alloc(0xF00, 4)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Alloc(0x1000, 4)
	if err != nil {
		t.Fatal(err)
	}
	if second != first+0xF00 || len(a.spans) != 1 {
		t.Errorf("second = %#x after %#x with %d spans; want one contiguous span", second, first, len(a.spans))
	}
}

func TestAutoFitsExactGap(t *testing.T) {
	im := mustOpen(t, autoELF(t, 0x1000, autoBase+0x2000))
	a := NewArena(im)
	a.Auto()
	if _, err := a.Alloc(0x1000, 4); err != nil {
		t.Errorf("exact fit refused: %v", err)
	}
}

func TestAutoGrowsPastPaddingWithoutWaste(t *testing.T) {
	in := autoELF(t, 0x1000, autoDataVA)
	binary.LittleEndian.PutUint32(in[52+16:], 0xFF0) // 0x10 bytes of padding
	binary.LittleEndian.PutUint32(in[52+20:], 0xFF0)
	in[0xFFF] = 0
	a := NewArena(mustOpen(t, in))
	a.Auto()
	va, err := a.Alloc(0x100, 4)
	if err != nil {
		t.Fatal(err)
	}
	if va != autoBase+0xFF0 || len(a.spans) != 1 {
		t.Errorf("va = %#x with %d spans; padding not merged with growth", va, len(a.spans))
	}
}

func TestExtendExecPreservesAddressing(t *testing.T) {
	im := mustOpen(t, autoELF(t, 0x1000, autoDataVA))
	before := append([]byte(nil), im.ReadVA(autoDataVA, 0x40)...)
	start, end, err := im.ExtendExec(0x10)
	if err != nil {
		t.Fatal(err)
	}
	if start != autoBase+0x1000 || end != start+0x1000 {
		t.Errorf("span = %#x..%#x", start, end)
	}
	if !bytes.Equal(im.ReadVA(autoDataVA, 0x40), before) {
		t.Error("data no longer reads back at its address")
	}
	if err := im.WriteVA(start, []byte{1, 2}); err != nil {
		t.Errorf("grown span not writable: %v", err)
	}
	if b := mustOpen(t, im.Bytes()).ReadVA(start, 2); !bytes.Equal(b, []byte{1, 2}) {
		t.Errorf("grown span after reopen = %x", b)
	}
}

func TestAutoYAML(t *testing.T) {
	var ps PatchSet
	if err := yaml.UnmarshalWithOptions([]byte("caves:\n  - auto: {}\n"), &ps, yaml.DisallowUnknownField()); err != nil {
		t.Fatal(err)
	}
	if len(ps.Caves) != 1 || ps.Caves[0].Auto == nil {
		t.Errorf("caves = %+v", ps.Caves)
	}
}
