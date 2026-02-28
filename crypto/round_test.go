package crypto

import (
	"bytes"
	"crypto/rand"
	"slices"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {

	buf := make([]byte, 128)
	_, _ = rand.Read(buf)

	buf2 := slices.Clone(buf)

	key := CustomKeySchedule(CustomKeys[0])
	CustomEncrypt(buf2, key)
	CustomDecrypt(buf2, key)
	if !bytes.Equal(buf, buf2) {
		t.Error("encrypt and decrypt not equal")
	}
}
