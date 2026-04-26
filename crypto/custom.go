package crypto

import (
	"encoding/binary"
	"math/bits"
)

// sbox[i] = GF8(i^83) mod 285
var sbox = func() (out [256]byte) {
	var exp [256]byte
	var log [256]byte

	x := byte(1)
	for i := 0; i < 255; i++ {
		exp[i] = x
		log[x] = byte(i)
		// multiply by G=2 in GF(2^8) mod 0x11d
		hi := x & 0x80
		x <<= 1
		if hi != 0 {
			x ^= 0x1d
		}
	}

	for i := 1; i < 256; i++ {
		out[i] = exp[(uint(log[i])*83)%255]
	}
	return out
}()

var expandedSBOX = func() (out [256][4]byte) {
	for i := range sbox {
		out[i][0] = bits.RotateLeft8(sbox[i], 3)
		out[i][1] = bits.RotateLeft8(sbox[i], 2)
		out[i][2] = bits.RotateLeft8(sbox[i], 1)
		out[i][3] = sbox[i]
	}
	return out
}()

const CustomRounds = 16

func CustomDecrypt(buf []byte, key [32]byte) {
	if len(buf)%8 != 0 {
		panic("buf must be a multiple of 8")
	}

	var state1, state2, tmp [4]byte

	for i := 0; i < len(buf); i += 8 {
		copy(state1[:], buf[i:])
		copy(state2[:], buf[i+4:])

		for j := range CustomRounds {
			round := (CustomRounds - 1) - j
			tmp = CustomRound(state1, [4]byte(key[(round&7)*4:]))

			tmp[0] ^= state2[0]
			tmp[1] ^= state2[1]
			tmp[2] ^= state2[2]
			tmp[3] ^= state2[3]

			state2, state1 = state1, tmp
		}

		copy(buf[i:], state2[:])
		copy(buf[i+4:], state1[:])
	}
}

func CustomEncrypt(buf []byte, key [32]byte) {
	if len(buf)%8 != 0 {
		panic("buf must be a multiple of 8")
	}

	var state1, state2, tmp [4]byte

	for i := 0; i < len(buf); i += 8 {
		copy(state1[:], buf[i:])
		copy(state2[:], buf[i+4:])

		for round := range CustomRounds {
			tmp = CustomRound(state1, [4]byte(key[(round&7)*4:]))

			tmp[0] ^= state2[0]
			tmp[1] ^= state2[1]
			tmp[2] ^= state2[2]
			tmp[3] ^= state2[3]

			state2, state1 = state1, tmp
		}

		copy(buf[i:], state2[:])
		copy(buf[i+4:], state1[:])
	}
}

func CustomRound(state, key [4]byte) [4]byte {
	state[0] = key[0] ^ state[0]
	state[1] = key[1] ^ state[1]
	state[2] = key[2] ^ state[2]
	state[3] = key[3] ^ state[3]

	// state[0] = bits.RotateLeft8(state[0], 0)
	state[1] = bits.RotateLeft8(state[1], 1)
	state[2] = bits.RotateLeft8(state[2], 3)
	state[3] = bits.RotateLeft8(state[3], 6)

	sub := expandedSBOX[state[0]^state[1]^state[2]^state[3]]

	// rotate state << 7 as a 32-bit
	binary.LittleEndian.PutUint32(state[:], bits.RotateLeft32(binary.LittleEndian.Uint32(state[:]), 7))

	state[0] += sub[0]
	state[1] += sub[1]
	state[2] += sub[2]
	state[3] += sub[3]
	return state
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
