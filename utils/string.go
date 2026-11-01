package utils

import "bytes"

// CString Extracts a nil-terminated C-style string from a buffer
func CString(buf []byte) string {
	if before, _, ok := bytes.Cut(buf, []byte{0}); ok {
		return string(before)
	}
	return string(buf)
}
