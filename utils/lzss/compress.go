package lzss

func Compress(data []byte, exhaustive bool) (output []byte, err error) {
	if len(data) == 0 {
		return nil, nil
	}

	windowIndex := WindowInitialIndex

	window := CreateWindow()

	output = make([]byte, 0, len(data))

	var countFlags int
	var flagsOffset int

	for len(data) > 0 {
		// Find maximum pattern in window
		if countFlags == 0 {
			countFlags = 8
			flagsOffset = len(output)
			output = append(output, 0)
		}

		offsetIndex, length := window.Find(windowIndex, data, exhaustive)

		if offsetIndex != -1 {
			// write false for not encoding a literal
			//output[flagsOffset] |= 0 << (8 - countFlags)

			// write index in table, size of match
			output = append(output, uint8(offsetIndex&0xFF))
			output = append(output, uint8((offsetIndex>>LengthBits)&0xF0)|uint8((length-MaxUncoded)&0x0F))

			windowIndex = window.Set(windowIndex, data[:length])
			data = data[length:]
		} else {
			// write true for encoding a literal
			output[flagsOffset] |= 1 << (8 - countFlags)

			literal := data[0]
			data = data[1:]
			windowIndex = window.SetByte(windowIndex, literal)

			// Write literal value
			output = append(output, literal)
		}

		countFlags--
	}

	return output, nil
}
