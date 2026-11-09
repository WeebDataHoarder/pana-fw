package elfpatch

import (
	"encoding/binary"
	"os"
	"slices"
	"strings"
	"testing"
)

// buildELF assembles a minimal little-endian ELF32 ARM executable with a single
// RX PT_LOAD mapping the whole file at vaddr base. It is enough to exercise
// Open, the string/pattern anchors and the edit actions without shipping any
// third-party binary.
func buildELF(base uint32, body []byte) []byte {
	const ehsize, phentsize = 52, 32
	total := ehsize + phentsize + len(body)
	b := make([]byte, total)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 1, 1, 1, 0})
	le := binary.LittleEndian
	le.PutUint16(b[16:], 2)                     // ET_EXEC
	le.PutUint16(b[18:], 40)                    // EM_ARM
	le.PutUint32(b[20:], 1)                     // version
	le.PutUint32(b[24:], base+ehsize+phentsize) // entry
	le.PutUint32(b[28:], ehsize)                // phoff
	le.PutUint32(b[32:], 0)                     // shoff
	le.PutUint32(b[36:], 0x05000000)            // flags (EABI5)
	le.PutUint16(b[40:], ehsize)
	le.PutUint16(b[42:], phentsize)
	le.PutUint16(b[44:], 1) // phnum
	// program header
	p := b[ehsize:]
	le.PutUint32(p[0:], 1)              // PT_LOAD
	le.PutUint32(p[4:], 0)              // offset
	le.PutUint32(p[8:], base)           // vaddr
	le.PutUint32(p[12:], base)          // paddr
	le.PutUint32(p[16:], uint32(total)) // filesz
	le.PutUint32(p[20:], uint32(total)) // memsz
	le.PutUint32(p[24:], 5)             // PF_R|PF_X
	le.PutUint32(p[28:], 0x10000)       // align
	copy(b[ehsize+phentsize:], body)
	return b
}

const (
	synthBase = 0x10000

	fnExpect   = "00 b5 00 bf 00 bf 00 bd"
	codeExpect = "70 b5 00 bf 00 bf 00 bf"
)

var fnBytes = []byte{0x00, 0xB5, 0x00, 0xBF, 0x00, 0xBF, 0x00, 0xBD}

func expectAt(data []byte, va uint32) string { return spacedHex(data[va-synthBase:][:8]) }

// bodyLayout returns the body and the VAs of interest.
func synthBody() (body []byte, codeVA, caveVA, strVA uint32) {
	// content offset within file is ehsize+phentsize = 84
	const contentOff = 84
	body = make([]byte, 0x400)
	// code at body offset 0x100: push {r4,lr}; nop; nop ... (unique "70 b5")
	copy(body[0x100:], []byte{0x70, 0xb5, 0x00, 0xbf, 0x00, 0xbf, 0x00, 0xbf})
	// cave at body offset 0x200: a dead function
	copy(body[0x200:], fnBytes)
	copy(body[0x280:], fnBytes)
	// external targets at 0x380 and 0x390
	copy(body[0x380:], fnBytes)
	copy(body[0x390:], fnBytes)
	// string at body offset 0x300
	copy(body[0x300:], append([]byte("HELLO"), 0))
	codeVA = synthBase + contentOff + 0x100
	caveVA = synthBase + contentOff + 0x200
	strVA = synthBase + contentOff + 0x300
	return
}

func TestOpenSynthetic(t *testing.T) {
	body, codeVA, _, strVA := synthBody()
	im, err := Open(buildELF(synthBase, body))
	if err != nil {
		t.Fatal(err)
	}
	tv, te := im.TextRange()
	if codeVA < tv || codeVA >= te {
		t.Errorf("codeVA %#x outside text %#x..%#x", codeVA, tv, te)
	}
	if got := im.FindString("HELLO"); len(got) != 1 || got[0] != strVA {
		t.Errorf("FindString(HELLO)=%v want [%#x]", got, strVA)
	}
}

func TestPokeAndNop(t *testing.T) {
	body, codeVA, _, _ := synthBody()
	data := buildELF(synthBase, body)

	ofs := 0
	ps := &PatchSet{Edits: []Edit{
		{Name: "str", Action: "poke",
			Anchor: Anchor{String: "HELLO"}, At: &Locator{Offset: &ofs},
			Write: "4a"}, // 'H' -> 'J'
		{Name: "nopout", Action: "nop", Count: 1,
			Anchor: Anchor{Pattern: "70 b5"}},
	}}
	out, err := ps.Apply(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	im, _ := Open(out)
	if got := im.FindString("JELLO"); len(got) != 1 {
		t.Errorf("poke did not rewrite string: %v", got)
	}
	// the push {r4,lr} (70 b5) must now be a nop (00 bf)
	if b := im.ReadVA(codeVA, 2); b == nil || b[0] != 0x00 || b[1] != 0xbf {
		t.Errorf("nop did not apply: %x", b)
	}
}

func TestDetourSynthetic(t *testing.T) {
	body, codeVA, caveVA, _ := synthBody()
	data := buildELF(synthBase, body)
	// hook.o imports getenv and orig_tail; resolve both to arbitrary mapped VAs.
	gv := uint32(synthBase + 84 + 0x380 + 1)
	ot := uint32(synthBase + 84 + 0x390 + 1) // Thumb
	ps := &PatchSet{
		Objects: map[string]string{"hook": "testdata/hook.o"},
		Caves:   []CaveSpec{{Range: &VARange{Start: caveVA, End: caveVA + 0x80, Expect: fnExpect}}},
		Edits: []Edit{{
			Name: "detour", Action: "detour",
			Anchor: Anchor{Pattern: "70 b5"},
			Object: "hook", Entry: "hook",
			Externals: map[string]ExtRef{
				"getenv":    {VA: &gv, Expect: fnExpect},
				"orig_tail": {VA: &ot, Expect: fnExpect},
			},
		}},
	}
	out, err := ps.Apply(data, func(p string) ([]byte, error) { return os.ReadFile(p) })
	if err != nil {
		t.Fatal(err)
	}
	im, _ := Open(out)
	// the site must now be a B.W into the cave
	k, ok := Decode(im.ReadVA(codeVA, 4), codeVA)
	if !ok || !k.Branch {
		t.Fatalf("site not a branch: %+v", k)
	}
	if k.Target < caveVA || k.Target >= caveVA+0x80 {
		t.Errorf("detour target %#x not in cave %#x..%#x", k.Target, caveVA, caveVA+0x80)
	}
	// the stub's entry at the cave must decode (first instruction of hook)
	if _, ok := Decode(im.ReadVA(k.Target, 4), k.Target); !ok {
		t.Errorf("stub at %#x does not decode", k.Target)
	}
}

func intp(n int) *int { return &n }

// TestExternalResolvesAgainstOriginal guards the planning-pass guarantee: a
// poke that truncates a string must not stop a later edit from resolving a
// `string:` external to that same (stock) string. Before the fix, pokes ran
// first and the external's FindString saw the truncated bytes.
func TestExternalResolvesAgainstOriginal(t *testing.T) {
	body, _, caveVA, strVA := synthBody()
	data := buildELF(synthBase, body)
	gv := uint32(synthBase + 84 + 0x380 + 1)
	ot := uint32(synthBase + 84 + 0x390 + 1)
	ps := &PatchSet{
		Objects: map[string]string{"hook": "testdata/hook.o"},
		Caves:   []CaveSpec{{Range: &VARange{Start: caveVA, End: caveVA + 0x80, Expect: fnExpect}}},
		// Poke runs first and truncates "HELLO" -> "HELL".
		Pokes: []Poke{{
			Name:   "trunc",
			Anchor: Anchor{String: "HELLO"},
			At:     &Locator{Offset: intp(4)},
			Write:  "00",
		}},
		Edits: []Edit{{
			Name: "detour", Action: "detour",
			Anchor: Anchor{Pattern: "70 b5"},
			Object: "hook", Entry: "hook",
			Externals: map[string]ExtRef{
				"getenv":    {VA: &gv, Expect: fnExpect},
				"orig_tail": {VA: &ot, Expect: fnExpect},
				// Resolved by FindString; must see the stock "HELLO".
				"feature": {String: "HELLO"},
			},
		}},
	}
	out, err := ps.Apply(data, func(p string) ([]byte, error) { return os.ReadFile(p) })
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	im, _ := Open(out)
	// The string is now truncated in the output, but the edit still applied.
	if got := im.ReadVA(strVA, 5); got[4] != 0 {
		t.Errorf("poke did not truncate HELLO: %q", got)
	}
}

// buildELF2 builds an ELF with an exec PT_LOAD followed by file padding and a
// second RW PT_LOAD, so GrowExecSlack has tail slack to claim.
func buildELF2(base uint32) []byte {
	const ehsize, phentsize = 52, 64 // two phdrs
	le := binary.LittleEndian
	execFilesz := uint32(ehsize + phentsize + 0x40) // header + a little code
	seg2Off := uint32(0x1000)                       // padding between the two in-file
	seg2 := make([]byte, 0x20)
	total := int(seg2Off) + len(seg2)
	b := make([]byte, total)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 1, 1, 1, 0})
	le.PutUint16(b[16:], 2)
	le.PutUint16(b[18:], 40)
	le.PutUint32(b[20:], 1)
	le.PutUint32(b[24:], base+ehsize+phentsize)
	le.PutUint32(b[28:], ehsize)
	le.PutUint16(b[40:], ehsize)
	le.PutUint16(b[42:], 32) // phentsize
	le.PutUint16(b[44:], 2)  // phnum
	p := b[ehsize:]
	// exec segment
	le.PutUint32(p[0:], 1)
	le.PutUint32(p[4:], 0)
	le.PutUint32(p[8:], base)
	le.PutUint32(p[12:], base)
	le.PutUint32(p[16:], execFilesz)
	le.PutUint32(p[20:], execFilesz)
	le.PutUint32(p[24:], 5) // RX
	le.PutUint32(p[28:], 0x10000)
	// rw segment at a higher page
	q := p[32:]
	le.PutUint32(q[0:], 1)
	le.PutUint32(q[4:], seg2Off)
	le.PutUint32(q[8:], base+0x10000)
	le.PutUint32(q[12:], base+0x10000)
	le.PutUint32(q[16:], uint32(len(seg2)))
	le.PutUint32(q[20:], uint32(len(seg2)))
	le.PutUint32(q[24:], 6) // RW
	le.PutUint32(q[28:], 0x10000)
	return b
}

func TestSlackGrow(t *testing.T) {
	base := uint32(0x10000)
	im, err := Open(buildELF2(base))
	if err != nil {
		t.Fatal(err)
	}
	execFilesz := uint32(52 + 64 + 0x40)
	wantAvail := uint32(0x1000) - execFilesz // file gap to seg2 (< va gap)
	start, end, err := im.GrowExecSlack(0x20)
	if err != nil {
		t.Fatal(err)
	}
	if start != base+execFilesz {
		t.Errorf("slack start %#x want %#x", start, base+execFilesz)
	}
	if end-start != wantAvail {
		t.Errorf("slack size %#x want %#x", end-start, wantAvail)
	}
	// the grown region must now be writable (file-backed) and executable
	if err := im.WriteVA(start, []byte{0x00, 0xbf}); err != nil {
		t.Errorf("grown region not writable: %v", err)
	}
	// re-open the edited image: the header change must persist and map
	im2, err := Open(im.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := im2.VAToOff(start); !ok {
		t.Errorf("grown VA %#x not mapped after reopen", start)
	}
	// asking for more than exists must fail, not silently truncate
	if _, _, err := im2.GrowExecSlack(0x10000); err == nil {
		t.Error("expected slack-too-small error")
	}
}

func TestPokesSection(t *testing.T) {
	body, codeVA, _, strVA := synthBody()
	data := buildELF(synthBase, body)
	// one poke by string anchor, one by raw va anchor
	ps := &PatchSet{Pokes: []Poke{
		{Name: "s", Anchor: Anchor{String: "HELLO"}, Write: "4a"},                    // H->J
		{Name: "v", Anchor: Anchor{VA: &codeVA, Expect: codeExpect}, Write: "00 bf"}, // push -> nop
	}}
	out, err := ps.Apply(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	im, _ := Open(out)
	if got := im.FindString("JELLO"); len(got) != 1 || got[0] != strVA {
		t.Errorf("string poke failed: %v", got)
	}
	if b := im.ReadVA(codeVA, 2); b[0] != 0x00 || b[1] != 0xbf {
		t.Errorf("va poke failed: %x", b)
	}
}

func TestDetourCover(t *testing.T) {
	// A detour whose At pattern spans two instructions must displace both, so
	// _ret lands past them and no partial instruction is left behind.
	body, codeVA, caveVA, _ := synthBody()
	// put movw r0,#0 ; movt r0,#0 (two 4-byte instrs = 8 bytes) at the code site
	copy(body[0x100:], []byte{0x40, 0xf2, 0x00, 0x00, 0xc0, 0xf2, 0x00, 0x00})
	data := buildELF(synthBase, body)
	ps := &PatchSet{
		Objects: map[string]string{"hook": "hook.o"},
		Caves:   []CaveSpec{{Range: &VARange{Start: caveVA, End: caveVA + 0x80, Expect: fnExpect}}},
		Edits: []Edit{{
			Action: "detour",
			Anchor: Anchor{VA: &codeVA, Expect: "40 f2 00 00 c0 f2 00 00"},
			Cover:  8,
			Object: "hook", Entry: "hook",
			Externals: map[string]ExtRef{
				"getenv": {VA: u32p(synthBase + 84 + 0x380 + 1), Expect: fnExpect}, "orig_tail": {VA: u32p(synthBase + 84 + 0x390 + 1), Expect: fnExpect},
			},
		}},
	}
	obj, err := os.ReadFile("testdata/hook.o")
	if err != nil {
		t.Fatal(err)
	}
	out, err := ps.Apply(data, func(p string) ([]byte, error) { return obj, nil })
	if err != nil {
		t.Fatal(err)
	}
	im, _ := Open(out)
	// bytes [codeVA+4, codeVA+8) must be a NOP (the movt was displaced), not the
	// original movt.
	if b := im.ReadVA(codeVA+4, 2); b[0] != 0x00 || b[1] != 0xbf {
		t.Errorf("cover did not displace second instruction: %x", b)
	}
}

func u32p(v uint32) *uint32 { return &v }

func TestAnchorAmbiguous(t *testing.T) {
	// two "70 b5" sites -> pattern anchor must refuse.
	body, _, _, _ := synthBody()
	copy(body[0x120:], []byte{0x70, 0xb5})
	ps := &PatchSet{Edits: []Edit{{Action: "nop", Anchor: Anchor{Pattern: "70 b5"}}}}
	if _, err := ps.Apply(buildELF(synthBase, body), nil); err == nil {
		t.Error("expected ambiguous-anchor error")
	}
}

func applyErr(t *testing.T, ps *PatchSet, data []byte) {
	t.Helper()
	if _, err := ps.Apply(data, func(string) ([]byte, error) { return os.ReadFile("testdata/hook.o") }); err == nil {
		t.Error("expected an error")
	}
}

func TestWriteMustStayInSegment(t *testing.T) {
	im, err := Open(buildELF2(0x10000))
	if err != nil {
		t.Fatal(err)
	}
	end := uint32(0x10000 + 52 + 64 + 0x40)
	if im.ReadVA(end-2, 4) != nil {
		t.Error("read crossed the end of the segment")
	}
	if err := im.WriteVA(end-2, make([]byte, 4)); err == nil {
		t.Error("write crossed the end of the segment")
	}
}

func TestOpenRejectsBadSegments(t *testing.T) {
	b := buildELF(synthBase, make([]byte, 16))
	binary.LittleEndian.PutUint32(b[52+16:], uint32(len(b)+1))
	binary.LittleEndian.PutUint32(b[52+20:], uint32(len(b)+1))
	if _, err := Open(b); err == nil {
		t.Error("segment past end of file was accepted")
	}
}

func TestSlackStopsAtNextPage(t *testing.T) {
	b := buildELF2(0x10000)
	// Map the next segment just above the text so its first page caps the grow.
	binary.LittleEndian.PutUint32(b[52+32+8:], 0x10000+0x400)
	if _, _, err := mustOpen(t, b).GrowExecSlack(1); err == nil {
		t.Error("grew into the page the next segment is mapped over")
	}
}

func TestSlackRefusesBSSTail(t *testing.T) {
	b := buildELF2(0x10000)
	binary.LittleEndian.PutUint32(b[52+20:], 0x8000) // memsz > filesz
	if _, _, err := mustOpen(t, b).GrowExecSlack(1); err == nil {
		t.Error("grew over a zero-initialised tail")
	}
}

func TestSlackStopsAtNonZero(t *testing.T) {
	b := buildELF2(0x10000)
	const fileEnd = 52 + 64 + 0x40
	b[fileEnd+0x10] = 0xAA
	start, end, err := mustOpen(t, b).GrowExecSlack(1)
	if err != nil {
		t.Fatal(err)
	}
	if end-start != 0x10 {
		t.Errorf("slack = %#x, want 0x10", end-start)
	}
}

func mustOpen(t *testing.T, b []byte) *Image {
	t.Helper()
	im, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	return im
}

func TestFuncDiscovery(t *testing.T) {
	body := make([]byte, 0x400)
	// push.w {r4, lr}; bx lr at 0x100, with a literal pool word that looks like
	// a prologue at 0x204 referenced from a ldr at 0x200.
	copy(body[0x100:], []byte{0x2D, 0xE9, 0x10, 0x40, 0x70, 0x47})
	copy(body[0x1FC:], []byte{0x00, 0xBF, 0x00, 0xBF, 0x00, 0x48, 0x70, 0x47, 0x00, 0xB5, 0x00, 0xB5})
	im := mustOpen(t, buildELF(synthBase, body))
	const contentVA = synthBase + 84
	if got := im.FuncStart(contentVA + 0x104); got != contentVA+0x100 {
		t.Errorf("push.w prologue missed: start %#x", got)
	}
	if slices.Contains(im.index().starts, uint32(contentVA+0x204)) {
		t.Error("literal pool word detected as a function start")
	}
}

func TestFuncExtent(t *testing.T) {
	body := make([]byte, 0x400)
	// 0x100: push {lr}; cbz r0,+2; pop {pc}; movs r0,#0; pop {pc}; next-function bytes.
	copy(body[0x100:], []byte{0x00, 0xB5, 0x08, 0xB1, 0x00, 0xBD, 0x00, 0x20, 0x00, 0xBD, 0x70, 0x47, 0x70, 0x47})
	im := mustOpen(t, buildELF(synthBase, body))
	const fn = synthBase + 84 + 0x100
	if got := im.funcExtent(fn); got != fn+10 {
		t.Errorf("extent = %#x, want %#x", got, uint32(fn+10))
	}
}

func TestAnchorNeedsOneSelector(t *testing.T) {
	im := mustOpen(t, buildELF(synthBase, make([]byte, 16)))
	if _, err := (Anchor{}).Resolve(im); err == nil {
		t.Error("empty anchor accepted")
	}
	va := uint32(synthBase)
	if _, err := (Anchor{VA: &va, String: "x"}).Resolve(im); err == nil {
		t.Error("two selectors accepted")
	}
}

func TestOverlappingEditsRejected(t *testing.T) {
	body, codeVA, caveVA, _ := synthBody()
	data := buildELF(synthBase, body)
	applyErr(t, &PatchSet{Pokes: []Poke{
		{Anchor: Anchor{VA: &codeVA, Expect: codeExpect}, Write: "00 bf"},
		{Anchor: Anchor{VA: u32p(codeVA + 1), Expect: "b5 00 bf 00 bf 00 bf 00"}, Write: "00"},
	}}, data)
	applyErr(t, &PatchSet{
		Caves: []CaveSpec{{Range: &VARange{Start: caveVA, End: caveVA + 0x80, Expect: fnExpect}}},
		Pokes: []Poke{{Anchor: Anchor{VA: u32p(caveVA + 8), Expect: "00 00 00 00 00 00 00 00"}, Write: "00"}},
	}, data)
}

func TestDetourSafety(t *testing.T) {
	const extVA = synthBase + 84 + 0x380
	stub := func(data []byte, va, caveVA uint32, cover int) *PatchSet {
		return &PatchSet{
			Objects: map[string]string{"h": "h"},
			Caves:   []CaveSpec{{Range: &VARange{Start: caveVA, End: caveVA + 0x80, Expect: fnExpect}}},
			Edits: []Edit{{Action: "detour", Anchor: Anchor{VA: &va, Expect: expectAt(data, va)}, Cover: cover, Object: "h", Entry: "hook",
				Externals: map[string]ExtRef{
					"getenv":    {VA: u32p(extVA + 1), Expect: fnExpect},
					"orig_tail": {VA: u32p(extVA + 0x11), Expect: fnExpect},
				}}},
		}
	}
	open := func(string) ([]byte, error) { return os.ReadFile("testdata/hook.o") }

	body, codeVA, caveVA, _ := synthBody()
	copy(body[0x100:], []byte{0x08, 0xBF, 0x00, 0x20, 0x00, 0xBF, 0x00, 0xBF}) // it eq; moveq r0,#0; nop; nop
	data := buildELF(synthBase, body)
	applyErr(t, stub(data, codeVA+2, caveVA, 4), data) // inside the IT block
	applyErr(t, stub(data, codeVA+1, caveVA, 4), data) // odd address
	if _, err := stub(data, codeVA+4, caveVA, 4).Apply(data, open); err != nil {
		t.Errorf("site after the IT block: %v", err)
	}

	body, codeVA, caveVA, _ = synthBody()
	copy(body[0x100:], []byte{0x00, 0xB5, 0x5F, 0xFA, 0x88, 0xF3, 0x08, 0xBF, 0x00, 0x20, 0x00, 0xBF, 0x00, 0xBF}) // push; uxtb.w; it eq; moveq; nop x2
	data = buildELF(synthBase, body)
	applyErr(t, stub(data, codeVA+8, caveVA, 4), data) // IT directly after a 32-bit instruction

	body, codeVA, caveVA, _ = synthBody()
	copy(body[0x100:], []byte{0x00, 0xB5, 0x00, 0xBD, 0x70, 0x47, 0x70, 0x47}) // push; pop {pc}; next function
	data = buildELF(synthBase, body)
	applyErr(t, stub(data, codeVA+2, caveVA, 4), data) // would overrun the return

	body, codeVA, caveVA, _ = synthBody()
	copy(body[0x100:], []byte{0x00, 0xB5, 0x00, 0xBF, 0x00, 0xBF, 0x00, 0xBF, 0xFB, 0xE7}) // push; nop x3; b back to site+2
	data = buildELF(synthBase, body)
	applyErr(t, stub(data, codeVA, caveVA, 6), data) // the branch lands inside the displaced bytes
	if _, err := stub(data, codeVA+6, caveVA, 4).Apply(data, open); err != nil {
		t.Errorf("site clear of the branch target: %v", err)
	}
}

func TestReturnsNeedCtx(t *testing.T) {
	body, codeVA, caveVA, _ := synthBody()
	applyErr(t, &PatchSet{
		Objects: map[string]string{"h": "h"},
		Caves:   []CaveSpec{{Range: &VARange{Start: caveVA, End: caveVA + 0x80, Expect: fnExpect}}},
		Edits:   []Edit{{Action: "hook", Anchor: Anchor{VA: &codeVA, Expect: codeExpect}, Object: "h", Returns: []ExtRef{{VA: u32p(codeVA + 5), Expect: "00 bf 00 bf 00 00 00 00"}}}},
	}, buildELF(synthBase, body))
}

func TestRawAddressesNeedExpect(t *testing.T) {
	va := uint32(0x11000)
	for _, ps := range []*PatchSet{
		{Pokes: []Poke{{Anchor: Anchor{VA: &va}, Write: "00"}}},
		{Caves: []CaveSpec{{Range: &VARange{Start: va, End: va + 8}}}},
		{Edits: []Edit{{Anchor: Anchor{String: "x"}, Externals: map[string]ExtRef{"f": {VA: u32p(va | 1)}}}}},
		{Edits: []Edit{{Anchor: Anchor{String: "x"}, Returns: []ExtRef{{VA: u32p(va | 1)}}}}},
	} {
		if err := ps.Validate(); err == nil {
			t.Errorf("accepted %+v", ps)
		}
	}
	if err := (&PatchSet{Edits: []Edit{{Anchor: Anchor{String: "x"}, Externals: map[string]ExtRef{"data": {VA: &va}}}}}).Validate(); err == nil {
		t.Error("data va without used_by accepted")
	}
	used := &Anchor{Export: "f"}
	if err := (&PatchSet{Edits: []Edit{{Anchor: Anchor{String: "x"}, Externals: map[string]ExtRef{"data": {VA: &va, UsedBy: used}}}}}).Validate(); err != nil {
		t.Errorf("data va with used_by: %v", err)
	}
	if err := (&PatchSet{Pokes: []Poke{{Anchor: Anchor{VA: &va, Expect: "00"}, At: &Locator{Offset: intp(2)}}}}).Validate(); err == nil {
		t.Error("va anchor with at accepted")
	}
}

func TestExpectNeedsEnoughFixedBytes(t *testing.T) {
	body, codeVA, _, _ := synthBody()
	data := buildELF(synthBase, body)
	poke := func(expect string) *PatchSet {
		return &PatchSet{Pokes: []Poke{{Anchor: Anchor{VA: &codeVA, Expect: expect}, Write: "00 bf"}}}
	}
	for expect, want := range map[string]string{
		"?? ?? ?? b?":             "mostly wildcards",
		"70 b5 ?? ?? ?? ?? ?? ??": "mostly wildcards",
		"70 b5 00 bf":             "needs at least 16",
	} {
		if _, err := poke(expect).Apply(data, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("expect %q: error = %v, want it to contain %q", expect, err, want)
		}
	}
}

func TestExpectMismatch(t *testing.T) {
	body, codeVA, _, _ := synthBody()
	data := buildELF(synthBase, body)
	bad := &PatchSet{Pokes: []Poke{{Anchor: Anchor{VA: &codeVA, Expect: "71 b5 00 bf 00 bf 00 bf"}, Write: "00 bf"}}}
	if _, err := bad.Apply(data, nil); err == nil || !strings.Contains(err.Error(), "wrong firmware") {
		t.Errorf("mismatch: %v", err)
	}
	ok := &PatchSet{Pokes: []Poke{{Anchor: Anchor{VA: &codeVA, Expect: "7? b5 00 bf 00 bf 00 bf 00"}, Write: "00 bf"}}}
	if _, err := ok.Apply(data, nil); err != nil {
		t.Errorf("masked expect: %v", err)
	}
}

// guardFixture has a caller at 0x100 of a function at 0x200.
func guardFixture() (body []byte, caller, callee uint32) {
	body = make([]byte, 0x400)
	copy(body[0x100:], []byte{0x00, 0xB5, 0x00, 0xF0, 0x7D, 0xF8, 0x00, 0xBD}) // push; bl 0x200; pop
	copy(body[0x200:], []byte{0x00, 0xB5, 0x00, 0xBF, 0x00, 0xBD})
	return body, synthBase + 84 + 0x100, synthBase + 84 + 0x200
}

func TestRangeRefusesLiveCode(t *testing.T) {
	body, _, callee := guardFixture()
	data := buildELF(synthBase, body)
	calleeExpect := "00 b5 00 bf 00 bd 00 00"
	applyErr(t, &PatchSet{Caves: []CaveSpec{{Range: &VARange{Start: callee, End: callee + 6, Expect: calleeExpect}}}}, data)                  // called
	applyErr(t, &PatchSet{Caves: []CaveSpec{{Range: &VARange{Start: callee + 2, End: callee + 6, Expect: "00 bf 00 bd 00 00 00 00"}}}}, data) // not an entry

	unreached := append([]byte(nil), body...)
	copy(unreached[0x100:], make([]byte, 8))
	copy(unreached[0x202:], []byte{0x00, 0xBF, 0x00, 0xBD})
	dead := &PatchSet{Caves: []CaveSpec{{Range: &VARange{Start: callee, End: callee + 6, Expect: calleeExpect}}}}
	if _, err := dead.Apply(buildELF(synthBase, unreached), nil); err != nil {
		t.Errorf("unreferenced range: %v", err)
	}
}

func TestRawExternalChecks(t *testing.T) {
	body, _, callee := guardFixture()
	im := mustOpen(t, buildELF(synthBase, body))
	if err := im.checkRawRef(ExtRef{VA: u32p(callee | 1)}); err != nil {
		t.Errorf("function entry: %v", err)
	}
	if err := im.checkRawRef(ExtRef{VA: u32p((callee + 2) | 1)}); err == nil {
		t.Error("mid-function address accepted as a function")
	}
	user := &Anchor{VA: &callee, Expect: "00 b5 00 bf 00 bd 00 00"}
	if err := im.checkRawRef(ExtRef{VA: u32p(callee + 0x40), UsedBy: user}); err == nil {
		t.Error("data address not used by the function was accepted")
	}
	if err := im.checkRawRef(ExtRef{VA: u32p(0xF0000000), UsedBy: user}); err == nil {
		t.Error("unmapped data address accepted")
	}
}

func TestPointerIntoSpan(t *testing.T) {
	const base = 0x10000
	b := buildELF2(base)
	binary.LittleEndian.PutUint32(b[0x1000:], base+0x101)
	im := mustOpen(t, b)
	if _, found := im.pointerInto(addrRange{base + 0x100, base + 0x104}); !found {
		t.Error("data pointer not found")
	}
	if _, found := im.pointerInto(addrRange{base + 0x200, base + 0x204}); found {
		t.Error("spurious data pointer")
	}
}
