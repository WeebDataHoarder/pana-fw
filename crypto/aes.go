package crypto

import (
	"crypto/aes"
	"crypto/cipher"
)

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
