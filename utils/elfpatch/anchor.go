package elfpatch

import (
	"fmt"
	"strings"
)

// Anchor locates a function or datum by a property that survives firmware
// rebuilds. Exactly one selector must be set.
type Anchor struct {
	Export  string  `yaml:"export,omitempty"`  // exported function
	StrXref string  `yaml:"strxref,omitempty"` // function referencing this string (exact, else substring)
	Pattern string  `yaml:"pattern,omitempty"` // function containing this code pattern
	String  string  `yaml:"string,omitempty"`  // address of this string itself
	VA      *uint32 `yaml:"va,omitempty"`      // fixed address; not version-portable
	Expect  string  `yaml:"expect,omitempty"`  // masked pattern required at the address; mandatory with va
}

func countTrue(flags ...bool) int {
	n := 0
	for _, set := range flags {
		if set {
			n++
		}
	}
	return n
}

// isData reports whether the anchor can point at non-code bytes.
func (a Anchor) isData() bool { return a.String != "" || a.VA != nil }

// single returns the only element of found, or an error naming what was sought.
func single[T any](what string, found []T) (T, error) {
	var zero T
	switch len(found) {
	case 0:
		return zero, fmt.Errorf("%s not found", what)
	case 1:
		return found[0], nil
	default:
		return zero, fmt.Errorf("%s matched %d times, not unique", what, len(found))
	}
}

// Resolve returns the anchored address, failing unless it is unambiguous.
func (a Anchor) Resolve(im *Image) (uint32, error) {
	va, err := a.resolve(im)
	if err != nil {
		return 0, err
	}
	if err := im.checkExpect(va, a.Expect); err != nil {
		return 0, err
	}
	return va, nil
}

func (a Anchor) resolve(im *Image) (uint32, error) {
	if n := countTrue(a.Export != "", a.StrXref != "", a.Pattern != "", a.String != "", a.VA != nil); n != 1 {
		return 0, fmt.Errorf("anchor needs exactly one selector, has %d", n)
	}
	switch {
	case a.VA != nil:
		if _, ok := im.VAToOff(*a.VA); !ok {
			return 0, fmt.Errorf("va %#x is not file-backed", *a.VA)
		}
		return *a.VA, nil
	case a.String != "":
		return single(fmt.Sprintf("string %q", a.String), im.FindString(a.String))
	case a.Export != "":
		va, thumb, ok := im.ExportVA(a.Export)
		if !ok {
			return 0, fmt.Errorf("export %q not found", a.Export)
		}
		if !thumb {
			return 0, fmt.Errorf("export %q is ARM state, only Thumb code is supported", a.Export)
		}
		return va, nil
	case a.StrXref != "":
		return single(fmt.Sprintf("strxref %q", a.StrXref), im.funcsReferencing(a.StrXref))
	default:
		return a.resolvePattern(im)
	}
}

func (a Anchor) resolvePattern(im *Image) (uint32, error) {
	p, err := ParsePattern(a.Pattern)
	if err != nil {
		return 0, err
	}
	start, end := im.TextRange()
	hit, err := single(fmt.Sprintf("anchor pattern %q", a.Pattern), p.FindAll(im.ReadVA(start, int(end-start))))
	if err != nil {
		return 0, err
	}
	return im.FuncStart(start + uint32(hit)), nil
}

// funcsReferencing prefers exact string matches, then falls back to substrings.
func (im *Image) funcsReferencing(s string) []uint32 {
	if fns := im.FuncsByString(s); len(fns) > 0 {
		return fns
	}
	seen := map[uint32]struct{}{}
	var out []uint32
	for str, fns := range im.index().strRefs {
		if !strings.Contains(str, s) {
			continue
		}
		for _, fn := range fns {
			if _, dup := seen[fn]; !dup {
				seen[fn] = struct{}{}
				out = append(out, fn)
			}
		}
	}
	return out
}

// Within locates pat inside the function at fn. occurrence is 1-based; 0
// requires a unique match.
func (im *Image) Within(fn uint32, pat *Pattern, occurrence int) (uint32, error) {
	end := im.FuncEnd(fn)
	if end <= fn {
		return 0, fmt.Errorf("function %#x is outside the scanned text", fn)
	}
	body := im.ReadVA(fn, int(end-fn))
	if body == nil {
		return 0, fmt.Errorf("function %#x is not file-backed", fn)
	}
	hits := pat.FindAll(body)
	what := fmt.Sprintf("pattern %q in function %#x", pat, fn)
	if occurrence <= 0 {
		off, err := single(what, hits)
		return fn + uint32(off), err
	}
	if occurrence > len(hits) {
		return 0, fmt.Errorf("%s has %d matches, no occurrence %d", what, len(hits), occurrence)
	}
	return fn + uint32(hits[occurrence-1]), nil
}
