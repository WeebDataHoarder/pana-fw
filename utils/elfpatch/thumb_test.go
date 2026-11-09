package elfpatch

import (
	"encoding/binary"
	"testing"
)

func TestInsnLen(t *testing.T) {
	cases := []struct {
		h    uint16
		want int
	}{
		{0x4800, 2}, // ldr literal
		{0xB500, 2}, // push {lr}
		{0xBF00, 2}, // nop
		{0xF000, 4}, // 32-bit (bl/b.w prefix)
		{0xE800, 4}, // 32-bit
		{0xF800, 4}, // 32-bit
		{0xE000, 2}, // b (unconditional T2), top5=11100 -> 16-bit
	}
	for _, c := range cases {
		if got := InsnLen(c.h); got != c.want {
			t.Errorf("InsnLen(%#x)=%d want %d", c.h, got, c.want)
		}
	}
}

func TestEncodeDecodeBranch(t *testing.T) {
	// Round-trip B.W and BL across a range of displacements, and confirm the
	// decoder recovers the target the encoder wrote.
	pc := uint32(0x10000)
	for _, tgt := range []uint32{0x10000, 0x10004, 0xF000, 0x18000, 0x10000 - 0x1000, 0x800000 + 0x10000} {
		for _, call := range []bool{false, true} {
			var h1, h2 uint16
			var ok bool
			if call {
				h1, h2, ok = EncodeBL(pc, tgt)
			} else {
				h1, h2, ok = EncodeBW(pc, tgt)
			}
			if !ok {
				t.Fatalf("encode %#x->%#x call=%v failed", pc, tgt, call)
			}
			var buf [4]byte
			binary.LittleEndian.PutUint16(buf[0:], h1)
			binary.LittleEndian.PutUint16(buf[2:], h2)
			k, ok := Decode(buf[:], pc)
			if !ok || !k.Branch || k.Target != tgt {
				t.Errorf("decode of %#x->%#x call=%v: branch=%v target=%#x", pc, tgt, call, k.Branch, k.Target)
			}
			if call && !k.Call {
				t.Errorf("BL %#x->%#x not decoded as call", pc, tgt)
			}
		}
	}
}

func TestEncodeBranchRange(t *testing.T) {
	if _, _, ok := EncodeBW(0x10000, 0x10000+(1<<25)); ok {
		t.Error("expected out-of-range branch to fail")
	}
	if _, _, ok := EncodeBW(0x10000, 0x10001); ok {
		t.Error("expected odd target to fail")
	}
}

func TestDecodeKinds(t *testing.T) {
	// IT, a literal load and a movw are flagged correctly.
	it := []byte{0x08, 0xBF} // it eq
	if k, _ := Decode(it, 0x100); k.ITCount != 1 {
		t.Error("IT not flagged")
	}
	ldr := []byte{0x00, 0x48} // ldr r0, [pc, #0]
	if k, _ := Decode(ldr, 0x100); !k.PCRel {
		t.Error("ldr literal not flagged PCRel")
	}
	// movw r0,#0 is 0xF240 0x0000; must NOT be PCRel
	movw := []byte{0x40, 0xF2, 0x00, 0x00}
	if k, _ := Decode(movw, 0x100); k.PCRel {
		t.Error("movw wrongly flagged PCRel")
	}
	for _, code := range [][]byte{
		{0x78, 0x46},             // mov r0, pc
		{0xDF, 0xE8, 0x00, 0xF0}, // tbb [pc, r0]
		{0xD0, 0xE9, 0x00, 0x00}, // not PC-relative: ldrd r0, r0, [r0]
	} {
		k, _ := Decode(code, 0x100)
		if want := code[0] != 0xD0; k.PCRel != want {
			t.Errorf("%x: PCRel=%v want %v", code, k.PCRel, want)
		}
	}
	// smc occupies the B<c>.W space with cond=111x and is not a branch.
	if k, _ := Decode([]byte{0xF0, 0xF7, 0x00, 0x80}, 0x100); k.Branch {
		t.Error("smc decoded as a branch")
	}
}
