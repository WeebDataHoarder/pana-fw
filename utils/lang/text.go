package lang

import (
	"errors"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
)

var errInvalid = errors.New("eucms: invalid byte sequence")

const (
	udcLead  = 0xf5
	udcFirst = 0xe000
	udcLast  = udcFirst + 10*94 - 1
)

func ToUTF8(src []byte) (string, error) {
	dec := japanese.EUCJP.NewDecoder()
	out := make([]byte, 0, len(src)*3/2)
	for i := 0; i < len(src); {
		c := src[i]
		n := 1
		switch {
		case c < 0x80:
			out = append(out, c)
			i++
			continue
		case c == 0x8f:
			n = 3
		case c == 0x8e || c >= 0xa1 && c <= 0xfe:
			n = 2
		default:
			return "", errInvalid
		}
		if i+n > len(src) {
			return "", errInvalid
		}
		seq := src[i : i+n]
		for _, t := range seq[1:] {
			if t < 0xa1 || t > 0xfe {
				return "", errInvalid
			}
		}
		if n == 2 && c >= udcLead {
			r := rune(udcFirst + int(c-udcLead)*94 + int(seq[1]-0xa1))
			out = utf8.AppendRune(out, r)
		} else {
			u, err := dec.Bytes(seq)
			if err != nil || string(u) == "\ufffd" {
				return "", errInvalid
			}
			out = append(out, u...)
		}
		i += n
	}
	return string(out), nil
}

// eucJP-ms code points that x/text decodes to a different character;
// the encoder accepts both.
var eucJPmsAlias = map[rune][2]byte{
	0x301c: {0xa1, 0xc1}, // 〜 (x/text: U+FF5E)
	0x2016: {0xa1, 0xc2}, // ‖ (U+2225)
	0x2014: {0xa1, 0xbd}, // — (U+2015)
	0x2212: {0xa1, 0xdd}, // − (U+FF0D)
	0x00a2: {0xa1, 0xf1}, // ¢ (U+FFE0)
	0x00a3: {0xa1, 0xf2}, // £ (U+FFE1)
	0x00ac: {0xa2, 0xcc}, // ¬ (U+FFE2)
}

func ToEUCJPms(s string) ([]byte, error) {
	enc := japanese.EUCJP.NewEncoder()
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r < 0x80:
			out = append(out, byte(r))
		case eucJPmsAlias[r] != [2]byte{}:
			b := eucJPmsAlias[r]
			out = append(out, b[0], b[1])
		case r >= udcFirst && r <= udcLast:
			k := int(r - udcFirst)
			out = append(out, byte(udcLead+k/94), byte(0xa1+k%94))
		default:
			b, err := enc.Bytes(utf8.AppendRune(nil, r))
			if err != nil {
				return nil, err
			}
			out = append(out, b...)
		}
	}
	return out, nil
}
