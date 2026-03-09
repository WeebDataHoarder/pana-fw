package crypto

import (
	"crypto/aes"
	"crypto/cipher"
)

var CustomKeys = []CustomKey{
	//B3A25A0B9D864F08 # 87 firmwares, ..-~2011
	{0x08, 0xE0, 0x3D, 0x85, 0x9A, 0xF9, 0xF3, 0xEE},
	//720FCB305F2F6EDA # 3 firmwares, ~2011
	{0xAF, 0x4D, 0x5B, 0x16, 0xC2, 0x98, 0xC1, 0x6D},
	//FBCC4CEA55466CA6 # 12 firmwares, ~2012
	{0x94, 0x07, 0x31, 0x1B, 0x75, 0x61, 0xF9, 0x7E},
	//8AC6E0CFF65651F5 # 8 firmwares, ~2013
	{0x38, 0xBB, 0x0C, 0x17, 0xD3, 0x62, 0x70, 0x1F},
	//5FF6D2A1B8675D6F # 3 firmwares, ~2013
	{0xEC, 0x23, 0x8C, 0xD7, 0x91, 0x45, 0x66, 0x46},
	//918C319720ACFEC1 # 5 firmwares, ~2014
	{0x8F, 0x8C, 0xA8, 0x84, 0x82, 0xE4, 0x3D, 0x9B},
	//542FD3A55F200FD5 # 5 firmwares, ~2014
	{0x21, 0xFC, 0xA1, 0x73, 0x61, 0xE6, 0x6B, 0x4E},
	//03075B3E4B43D4E9 # 1 firmware, ~2012
	{0x5D, 0x88, 0x71, 0x97, 0x45, 0xA4, 0xEF, 0xFF},

	//# 8 firmwares, 2018, AES
	{0x06, 0xC9, 0x43, 0xF3, 0xB9, 0x97, 0xF7, 0xE0},
}

var AESKeys = []AESKey{
	// # 8 firmwares, 2018
	NewAESKey(
		[16]byte{0x62, 0xA3, 0x9E, 0x1C, 0x55, 0x94, 0xAE, 0x09, 0x24, 0x4E, 0xB3, 0x26, 0xEF, 0x79, 0x38, 0xFA},
		[16]byte{0x46, 0xD0, 0x3C, 0x52, 0xB1, 0xA3, 0x98, 0xFA, 0x03, 0x27, 0x6F, 0xDE, 0x7C, 0xC4, 0xEA, 0x26},
		CustomKey{0x06, 0xC9, 0x43, 0xF3, 0xB9, 0x97, 0xF7, 0xE0},
	),
}

type AESKey struct {
	Key [16]byte
	IV  [16]byte
	// Custom Key result of two xored inputs
	Custom            CustomKey
	ExpandedCustomKey ExpandedCustomKey
}

func (k AESKey) Encrypt(data []byte) {
	c, _ := aes.NewCipher(k.Key[:])
	b := cipher.NewCBCEncrypter(c, k.IV[:])
	b.CryptBlocks(data, data)
}

func (k AESKey) EncryptCustom(data []byte) {
	k.ExpandedCustomKey.Encrypt(data)
	k.Encrypt(data)
}

func (k AESKey) Decrypt(data []byte) {
	c, _ := aes.NewCipher(k.Key[:])
	b := cipher.NewCBCDecrypter(c, k.IV[:])
	b.CryptBlocks(data, data)
}

func (k AESKey) DecryptCustom(data []byte) {
	k.Decrypt(data)
	k.ExpandedCustomKey.Decrypt(data)
}

func NewAESKey(key, iv [16]byte, custom CustomKey) AESKey {
	return AESKey{
		Key:               key,
		IV:                iv,
		Custom:            custom,
		ExpandedCustomKey: custom.Expand(),
	}
}

// CustomKey The result of two 8x8 byte keys, XOR'd together in a special way
// Each key is then reduced to 56 bit security on key expansion
type CustomKey [8]byte

func (k CustomKey) Expand() ExpandedCustomKey {
	return CustomKeySchedule(k)
}

type ExpandedCustomKey [32]byte

func (k ExpandedCustomKey) Encrypt(data []byte) {
	CustomEncrypt(data, k)
}
func (k ExpandedCustomKey) Decrypt(data []byte) {
	CustomDecrypt(data, k)
}

func CustomKeySchedule(xorInput CustomKey) (key [32]byte) {
	k := xorInput
	k[7] = 0
	// 56-bit reduction
	k[1] ^= xorInput[7]
	k[4] ^= xorInput[7]

	copy(key[:7], k[:])

	keyVector := [4]byte{0x35, 0x7D, 0xFB, 0x9A}

	for round := range CustomRounds {
		offset := (round & 7) * 4
		keyVector = CustomRound([4]byte(key[offset:]), keyVector)
		copy(key[offset:], keyVector[:])
	}
	return key
}
