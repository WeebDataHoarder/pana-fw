package elfpatch

import (
	"fmt"
	"strings"
)

// Arena hands out free space inside executable, file-backed parts of the host.
type Arena struct {
	im    *Image
	spans []span
	gaps  []addrRange // alignment holes left by allocations

	auto       bool // create space on demand when the spans run out
	slackTried bool
}

type span struct {
	start, next, end uint32
	label            string
}

// NewArena creates an empty arena over im.
func NewArena(im *Image) *Arena { return &Arena{im: im} }

func (a *Arena) addSpan(start, end uint32, label string) error {
	if !a.im.execFileBacked(start, end) {
		return fmt.Errorf("cave %q (%#x..%#x) is not a non-empty executable, file-backed span", label, start, end)
	}
	for _, s := range a.spans {
		if start < s.end && s.start < end {
			return fmt.Errorf("cave %q (%#x..%#x) overlaps %q (%#x..%#x)", label, start, end, s.label, s.start, s.end)
		}
	}
	a.spans = append(a.spans, span{start: start, next: start, end: end, label: label})
	return nil
}

// Reclaim frees the body of the function selected by anc, which the author
// declares dead.
func (a *Arena) Reclaim(anc Anchor) error {
	fn, err := anc.Resolve(a.im)
	if err != nil {
		return fmt.Errorf("reclaim: %w", err)
	}
	end := a.im.funcExtent(fn)
	if err := a.im.checkDead(addrRange{fn, end}); err != nil {
		return fmt.Errorf("reclaim: %w", err)
	}
	return a.addSpan(fn, end, fmt.Sprintf("reclaim %#x", fn))
}

// Range frees an explicit [start,end) span, which must run between function
// boundaries, hold the expected bytes and be unreachable from outside.
func (a *Arena) Range(start, end uint32, expect string) error {
	if err := a.im.checkExpect(start, expect); err != nil {
		return fmt.Errorf("range: %w", err)
	}
	if err := a.im.checkFunction(start); err != nil {
		return fmt.Errorf("range: %w", err)
	}
	if err := a.im.checkSpanEnd(end); err != nil {
		return fmt.Errorf("range: %w", err)
	}
	if err := a.im.checkDead(addrRange{start, end}); err != nil {
		return fmt.Errorf("range: %w", err)
	}
	return a.addSpan(start, end, fmt.Sprintf("range %#x..%#x", start, end))
}

// Slack frees the padding after the executable segment. minSize is the least
// usable size; the segment's program header is edited to map it.
func (a *Arena) Slack(minSize uint32) error {
	start, end, err := a.im.GrowExecSlack(minSize)
	if err != nil {
		return fmt.Errorf("slack: %w", err)
	}
	return a.addSpan(start, end, fmt.Sprintf("slack %#x..%#x", start, end))
}

// Auto lets Alloc create space when the declared caves are exhausted.
func (a *Arena) Auto() { a.auto = true }

// addOrExtend registers a new span, joining it to one that ends where it starts.
func (a *Arena) addOrExtend(start, end uint32, label string) error {
	for i := range a.spans {
		if a.spans[i].end == start {
			a.spans[i].end = end
			return nil
		}
	}
	return a.addSpan(start, end, label)
}

// expand claims the executable segment's existing padding once, and grows the
// segment if that does not fit size bytes.
func (a *Arena) expand(size, align uint32) error {
	if !a.slackTried {
		a.slackTried = true
		if start, end, err := a.im.GrowExecSlack(0); err == nil {
			if err := a.addOrExtend(start, end, "auto padding"); err != nil {
				return err
			}
			if _, ok := a.find(size, align); ok {
				return nil
			}
		}
	}
	vaEnd := a.im.firstExec().vaEnd()
	next := vaEnd
	for _, s := range a.spans {
		if s.end == vaEnd {
			next = s.next
		}
	}
	start, end, err := a.im.ExtendExec(alignUp(next, align) + size - vaEnd)
	if err != nil {
		return err
	}
	return a.addOrExtend(start, end, "auto growth")
}

// Alloc reserves size bytes, choosing the span that wastes the least padding.
func (a *Arena) Alloc(size, align uint32) (uint32, error) {
	best, ok := a.find(size, align)
	if !ok && a.auto {
		if err := a.expand(size, align); err != nil {
			return 0, fmt.Errorf("auto cave: %w", err)
		}
		best, ok = a.find(size, align)
	}
	if !ok {
		return 0, fmt.Errorf("no cave fits %d bytes (align %d); free: %s", size, align, a.freeReport())
	}
	s := &a.spans[best]
	va := alignUp(s.next, align)
	if va > s.next {
		a.gaps = append(a.gaps, addrRange{s.next, va})
	}
	s.next = va + size
	return va, nil
}

// find returns the index of the span that wastes the least padding.
func (a *Arena) find(size, align uint32) (int, bool) {
	best := -1
	var bestWaste uint32
	for i := range a.spans {
		s := &a.spans[i]
		va := alignUp(s.next, align)
		if uint64(va)+uint64(size) <= uint64(s.end) && (best < 0 || va-s.next < bestWaste) {
			best, bestWaste = i, va-s.next
		}
	}
	return best, best >= 0
}

// unused lists every byte of the caves that holds no stub.
func (a *Arena) unused() []addrRange {
	out := append([]addrRange(nil), a.gaps...)
	for _, s := range a.spans {
		if s.next < s.end {
			out = append(out, addrRange{s.next, s.end})
		}
	}
	return out
}

func (a *Arena) freeReport() string {
	if len(a.spans) == 0 {
		return "(none)"
	}
	var parts []string
	for _, s := range a.spans {
		parts = append(parts, fmt.Sprintf("[%s: %d B]", s.label, s.end-s.next))
	}
	return strings.Join(parts, " ")
}
