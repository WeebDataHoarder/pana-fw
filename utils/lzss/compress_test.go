package lzss

import (
	"bytes"
	"os"
	"slices"
	"testing"
)

var testData []byte

func init() {
	testData, _ = os.ReadFile("/usr/share/dict/words")
	if len(testData) == 0 {
		testData = []byte{
			0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
			0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
			15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0,
			0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
		}
		testData = slices.Repeat(testData, 16)
	}
}

func TestCompressRoundtrip(t *testing.T) {
	buf, err := Compress(testData, false)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("compress %d -> %d bytes (ratio %.02f%%)", len(testData), len(buf), 100-(float64(len(buf))/float64(len(testData)))*100)

	dec, err := Decompress(buf, 0, 1024*1024*32)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(testData, dec) {
		t.Error("mismatched decompressed data")
	}
}

func TestCompressExhaustiveRoundtrip(t *testing.T) {
	buf, err := Compress(testData, true)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("compress %d -> %d bytes (ratio %.02f%%)", len(testData), len(buf), 100-(float64(len(buf))/float64(len(testData)))*100)

	dec, err := Decompress(buf, 0, 1024*1024*32)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(testData, dec) {
		t.Error("mismatched decompressed data")
	}
}
