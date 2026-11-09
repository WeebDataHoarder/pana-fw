package elfpatch

import "testing"

func TestPatternMatch(t *testing.T) {
	p, err := ParsePattern("50 ?? c9")
	if err != nil {
		t.Fatal(err)
	}
	if p.Len() != 3 {
		t.Fatalf("len=%d", p.Len())
	}
	buf := []byte{0x00, 0x50, 0x22, 0xc9, 0x50, 0x23, 0xc9, 0xff}
	got := p.FindAll(buf)
	want := []int{1, 4}
	if len(got) != len(want) || got[0] != 1 || got[1] != 4 {
		t.Errorf("FindAll=%v want %v", got, want)
	}
}

func TestPatternNibbleWildcard(t *testing.T) {
	p, err := ParsePattern("4? f?")
	if err != nil {
		t.Fatal(err)
	}
	if !p.matchAt([]byte{0x4a, 0xf8}, 0) {
		t.Error("expected 4a f8 to match 4? f?")
	}
	if p.matchAt([]byte{0x5a, 0xf8}, 0) {
		t.Error("5a must not match 4?")
	}
}

func TestPatternErrors(t *testing.T) {
	if _, err := ParsePattern(""); err == nil {
		t.Error("empty pattern should error")
	}
	if _, err := ParsePattern("zz"); err == nil {
		t.Error("bad hex should error")
	}
	if _, err := ParsePattern("1"); err == nil {
		t.Error("odd token should error")
	}
}
