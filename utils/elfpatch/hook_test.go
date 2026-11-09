package elfpatch

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRegNum(t *testing.T) {
	cases := map[string]int{"r0": 0, "r12": 12, "sp": 13, "r13": 13, "r14": 14, "lr": 14, "R5": 5}
	for s, want := range cases {
		if n, err := regNum(s); err != nil || n != want {
			t.Errorf("regNum(%q)=%d,%v want %d", s, n, err, want)
		}
	}
	for _, bad := range []string{"r13x", "r1x", "pc", "r99", "x"} {
		if _, err := regNum(bad); err == nil {
			t.Errorf("regNum(%q) should fail", bad)
		}
	}
}

func TestLeanGlue(t *testing.T) {
	const glueVA, stub, ret = 0x10000, 0x10100, 0x20000
	g, err := leanGlue(stub, nil, 1, ret)
	if err != nil {
		t.Fatal(err)
	}
	b, err := g.link(glueVA)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte{0x08, 0xB5}) {
		t.Errorf("lean glue does not save lr: %x", b[:2])
	}
	if k, _ := Decode(b[2:], glueVA+2); !k.Call || k.Target != stub {
		t.Errorf("first insn: call=%v target=%#x want call to %#x", k.Call, k.Target, uint32(stub))
	}
	if k, _ := Decode(b[12:], glueVA+12); !k.Branch || k.Target != ret {
		t.Errorf("tail branch target %#x want %#x", k.Target, uint32(ret))
	}
}

func TestGlueRange(t *testing.T) {
	g, err := leanGlue(0x4000000, nil, 0, 0x20000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.link(0x10000); err == nil {
		t.Error("out-of-range hook call must fail")
	}
	// A return far from address zero must not fail just because of sizing.
	g, err = leanGlue(0x2a370000, nil, 0, 0x2a37c69a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.link(0x2a371000); err != nil {
		t.Error(err)
	}
}

// runMoves interprets the emitted mov sequence over registers holding their own number.
func runMoves(t *testing.T, args []int) [16]int {
	t.Helper()
	a := &thumbAsm{}
	emitArgMoves(a, args)
	var r [16]int
	for i := range r {
		r[i] = i
	}
	for i := 0; i < len(a.code); i += 2 {
		h := int(a.code[i]) | int(a.code[i+1])<<8
		r[h&7|h>>4&8] = r[h>>3&15]
	}
	return r
}

func TestArgMovesParallel(t *testing.T) {
	for _, args := range [][]int{{1, 0}, {2, 3, 0, 1}, {1, 2, 3, 0}, {3, 0}, {5, 0, 1}} {
		r := runMoves(t, args)
		for dst, src := range args {
			if r[dst] != src {
				t.Errorf("args %v: r%d = %d, want %d", args, dst, r[dst], src)
			}
		}
	}
}

func TestCtxGlueTargets(t *testing.T) {
	targets := []uint32{0x30001, 0x40001}
	b, err := ctxGlue(0x10200, targets).link(0x10000)
	if err != nil {
		t.Fatal(err)
	}
	tail := b[len(b)-8:]
	for i, want := range targets {
		if got := binary.LittleEndian.Uint32(tail[i*4:]); got != want {
			t.Errorf("target[%d]=%#x want %#x", i, got, want)
		}
	}
	// The epilogue must restore lr as well as r0-r12.
	if !bytes.Contains(b, []byte{0xBD, 0xE8, 0xFF, 0x5F}) {
		t.Error("ctx glue does not pop r0-r12 and lr")
	}
}
