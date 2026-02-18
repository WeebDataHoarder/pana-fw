package lzss

const DataMaxSize = 1024 * 1024 * 32

const LiteralBits = 8

// OffsetBits Number of bits for the Window offset. From this the WindowSize will be calculated
const OffsetBits = 12
const LengthBits = 4

const WindowSize = 1 << OffsetBits

const MaxUncoded = 3
const MaxCoded = MaxUncoded + (1 << LengthBits)

// WindowInitialIndex The windowIndex needs to be set to this before compression/decompression
// In other LZSS implementations, this is usually set to 0
const WindowInitialIndex = WindowSize - 0x10 - 2
