package crypto

import (
	"encoding/hex"
	"testing"
)

func TestCustomKeySchedule(t *testing.T) {
	out := CustomKeySchedule(CustomKeys[0])
	t.Logf("%x", out)
	if hex.EncodeToString(out[:]) != "5d1084b53820d6785b93e531309249a4f9a5e837515799c87eda9caf238722ef" {
		t.Fatal("mismatch")
	}
}

func TestCustomKeyDecrypt(t *testing.T) {
	key := CustomKeySchedule(CustomKeys[0])
	buf, _ := hex.DecodeString("B3A25A0B9D864F08")
	CustomDecrypt(buf, key)
	t.Logf("%x", buf)
	if hex.EncodeToString(buf[:]) != "0000000000000000" {
		t.Fatal("mismatch")
	}
}
