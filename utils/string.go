package utils

import "bytes"

// CString Extracts a nil-terminated C-style string from a buffer
func CString(buf []byte) string {
	if i := bytes.IndexByte(buf, 0); i != -1 {
		return string(buf[:i])
	}
	return string(buf)
}
