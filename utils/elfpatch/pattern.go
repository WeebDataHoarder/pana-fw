package elfpatch

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// Pattern is a masked byte pattern. The text form is whitespace-separated
// two-character tokens of hex nibbles, with '?' or '.' as a nibble wildcard.
type Pattern struct {
	val, mask []byte
	text      string
}

// ParsePattern compiles a textual pattern.
func ParsePattern(s string) (*Pattern, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty pattern")
	}
	p := &Pattern{text: s}
	for _, tok := range fields {
		if len(tok) != 2 {
			return nil, fmt.Errorf("pattern token %q must be two chars", tok)
		}
		var v, m byte
		for i := range 2 {
			m <<= 4
			v <<= 4
			if tok[i] == '?' || tok[i] == '.' {
				continue
			}
			d, err := hex.DecodeString("0" + tok[i:i+1])
			if err != nil {
				return nil, fmt.Errorf("bad nibble in pattern token %q", tok)
			}
			m |= 0xF
			v |= d[0]
		}
		p.val = append(p.val, v)
		p.mask = append(p.mask, m)
	}
	return p, nil
}

// Len is the pattern length in bytes.
func (p *Pattern) Len() int { return len(p.val) }

func (p *Pattern) String() string { return p.text }

func (p *Pattern) matchAt(buf []byte, i int) bool {
	for k := range p.val {
		if buf[i+k]&p.mask[k] != p.val[k] {
			return false
		}
	}
	return true
}

// FindAll returns every offset in buf where the pattern matches.
func (p *Pattern) FindAll(buf []byte) []int {
	var out []int
	for i := 0; i+len(p.val) <= len(buf); i++ {
		if p.matchAt(buf, i) {
			out = append(out, i)
		}
	}
	return out
}

// apply overlays the pattern on orig, keeping the original wildcard nibbles.
func (p *Pattern) apply(orig []byte) []byte {
	out := make([]byte, len(p.val))
	for i := range out {
		out[i] = orig[i]&^p.mask[i] | p.val[i]
	}
	return out
}
