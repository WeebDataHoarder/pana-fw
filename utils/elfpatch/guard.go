package elfpatch

import (
	"encoding/hex"
	"fmt"
	"math/bits"
)

// Validate rejects a patch set whose raw addresses do not say what must be
// there. Raw addresses do not survive a rebuild, and bytes at a stale address
// can still decode as plausible instructions.
func (ps *PatchSet) Validate() error {
	for i, c := range ps.Caves {
		if c.Range != nil && c.Range.Expect == "" {
			return fmt.Errorf("cave #%d: range needs expect", i+1)
		}
		if c.Reclaim != nil {
			if err := c.Reclaim.validate(); err != nil {
				return fmt.Errorf("cave #%d: %w", i+1, err)
			}
		}
	}
	for i, poke := range ps.Pokes {
		e := Edit{Anchor: poke.Anchor, At: poke.At}
		if err := e.validate(); err != nil {
			return fmt.Errorf("poke #%d %q: %w", i+1, poke.Name, err)
		}
	}
	for i := range ps.Edits {
		if err := ps.Edits[i].validate(); err != nil {
			return fmt.Errorf("edit #%d %q: %w", i+1, ps.Edits[i].Name, err)
		}
	}
	return nil
}

func (a Anchor) validate() error {
	if a.VA != nil && a.Expect == "" {
		return fmt.Errorf("va %#x needs expect", *a.VA)
	}
	return nil
}

func (r ExtRef) validate() error {
	switch {
	case r.VA == nil && (r.Expect != "" || r.UsedBy != nil):
		return fmt.Errorf("expect and used_by only apply to a va reference")
	case r.VA == nil:
		return nil
	case *r.VA&1 != 0 && r.UsedBy != nil:
		return fmt.Errorf("code va %#x takes expect, not used_by", *r.VA)
	case *r.VA&1 != 0 && r.Expect == "":
		return fmt.Errorf("code va %#x needs expect", *r.VA)
	case *r.VA&1 == 0 && r.UsedBy == nil:
		return fmt.Errorf("data va %#x needs used_by", *r.VA)
	case r.UsedBy != nil:
		return r.UsedBy.validate()
	default:
		return nil
	}
}

func (e *Edit) validate() error {
	if e.Cover < 0 {
		return fmt.Errorf("cover %d is negative", e.Cover)
	}
	if err := e.Anchor.validate(); err != nil {
		return err
	}
	if e.Anchor.VA != nil && e.At != nil {
		return fmt.Errorf("va %#x cannot take at; name the exact address", *e.Anchor.VA)
	}
	if e.At != nil && e.At.Offset != nil {
		if s := e.Anchor.String; s != "" {
			if *e.At.Offset < 0 || *e.At.Offset > len(s) {
				return fmt.Errorf("at.offset %d is outside string %q", *e.At.Offset, s)
			}
		} else if *e.At.Offset != 0 {
			return fmt.Errorf("at.offset %d from a code anchor is unpinned; use at.pattern", *e.At.Offset)
		}
	}
	for name, r := range e.Externals {
		if err := r.validate(); err != nil {
			return fmt.Errorf("external %q: %w", name, err)
		}
	}
	for i, r := range e.Returns {
		if err := r.validate(); err != nil {
			return fmt.Errorf("return #%d: %w", i+1, err)
		}
	}
	return nil
}

// checkExpect verifies the masked pattern against the bytes at va.
func (im *Image) checkExpect(va uint32, expect string) error {
	if expect == "" {
		return nil
	}
	pat, err := parseExpect(expect)
	if err != nil {
		return err
	}
	got := im.ReadVA(va, pat.Len())
	if got == nil {
		return fmt.Errorf("expected %q at %#x, which is not file-backed", expect, va)
	}
	if !pat.matchAt(got, 0) {
		return fmt.Errorf("expected %q at %#x, found %q; wrong firmware for this patch?", expect, va, spacedHex(got))
	}
	return nil
}

// minExpectNibbles is the least an expect pattern must fix. A single 4-byte
// instruction recurs throughout a binary, so it identifies nothing.
const minExpectNibbles = 16

// parseExpect compiles an expect pattern, which must fix enough of its nibbles
// to be evidence of anything: at least half, and at least minExpectNibbles.
func parseExpect(expect string) (*Pattern, error) {
	pat, err := ParsePattern(expect)
	if err != nil {
		return nil, fmt.Errorf("expect: %w", err)
	}
	fixed := 0
	for _, m := range pat.mask {
		fixed += bits.OnesCount8(m) / 4
	}
	if fixed < pat.Len() {
		return nil, fmt.Errorf("expect %q is mostly wildcards", expect)
	}
	if fixed < minExpectNibbles {
		return nil, fmt.Errorf("expect %q fixes %d nibbles, needs at least %d", expect, fixed, minExpectNibbles)
	}
	return pat, nil
}

func spacedHex(b []byte) string {
	out := make([]byte, 0, len(b)*3)
	for i, c := range b {
		if i > 0 {
			out = append(out, ' ')
		}
		out = hex.AppendEncode(out, []byte{c})
	}
	return string(out)
}

// checkFunction verifies that a raw Thumb address is a function entry.
func (im *Image) checkFunction(va uint32) error {
	if !im.isFuncStart(va) {
		return fmt.Errorf("%#x is not a function entry", va)
	}
	return nil
}

// checkRawRef applies the checks for a va: external. A set bit 0 names Thumb
// code, which must be a function entry; otherwise it names data, which UsedBy
// must use: the exact instruction for a va anchor, else the anchored function.
func (im *Image) checkRawRef(r ExtRef) error {
	va := *r.VA
	if va&1 != 0 {
		return im.checkFunction(va &^ 1)
	}
	if im.text.contains(va) {
		return fmt.Errorf("address %#x is code; set bit 0 for a Thumb target", va)
	}
	if !im.mapped(va) {
		return fmt.Errorf("data address %#x is not mapped", va)
	}
	user, err := r.UsedBy.Resolve(im)
	if err != nil {
		return fmt.Errorf("used_by: %w", err)
	}
	if !im.usedBy(va, user, r.UsedBy.VA != nil) {
		return fmt.Errorf("data address %#x is not used at %#x", va, user)
	}
	return nil
}

// checkReturn verifies that a raw return target resumes the site's function.
func (im *Image) checkReturn(site, target uint32) error {
	if fn, want := im.FuncStart(target&^1), im.FuncStart(site); fn != want {
		return fmt.Errorf("return target %#x is in function %#x, the site is in %#x", target, fn, want)
	}
	return nil
}

// checkSpanEnd verifies that a cave ends at a function entry, or after an
// instruction that does not fall through and before literals or padding.
func (im *Image) checkSpanEnd(end uint32) error {
	if im.isFuncStart(end) {
		return nil
	}
	for _, n := range []uint32{2, 4} {
		if k, ok := im.insnAt(end - n); ok && uint32(k.Len) == n && k.Terminal && (im.isLiteral(end) || im.isPadding(end)) {
			return nil
		}
	}
	return fmt.Errorf("span end %#x is not a function boundary", end)
}

// checkDead verifies that nothing outside r reaches into it, so that the span
// can be reused.
func (im *Image) checkDead(r addrRange) error {
	if from, found := im.referenceInto(r); found {
		return fmt.Errorf("span %#x..%#x is referenced by code at %#x", r.start, r.end, from)
	}
	if at, found := im.pointerInto(r); found {
		return fmt.Errorf("span %#x..%#x is pointed to by data at %#x", r.start, r.end, at)
	}
	return nil
}
