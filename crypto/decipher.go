package crypto

import (
	"crypto/aes"
	"crypto/cipher"
)

// NetworkDecipher Implementation compatible with decipher on libdownloadlib.so
func NetworkDecipher(data []byte) {
	const iv uint32 = 904

	var counter uint32
	for i := range data {
		if i&0xFF == 0 {
			counter = iv
		}

		v := data[i] ^ uint8(counter>>8)
		counter += (uint32(data[i]) + 38400) + 163
		data[i] = v
	}
}

func NetworkEncrypt(data []byte) {
	b, _ := aes.NewCipher(NetworkKeyIV[0][:])
	c := cipher.NewCBCEncrypter(b, NetworkKeyIV[1][:])
	c.CryptBlocks(data, data)
}

func NetworkDecrypt(data []byte) {
	b, _ := aes.NewCipher(NetworkKeyIV[0][:])
	c := cipher.NewCBCDecrypter(b, NetworkKeyIV[1][:])
	c.CryptBlocks(data, data)
}
