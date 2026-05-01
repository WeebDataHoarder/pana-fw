package lzss

// Compress compresses data using LZSS
// If exhaustive is true, the match finder walks the full hash chain to find the best match
func Compress(data []byte, exhaustive bool) (output []byte, err error) {
	n := len(data)
	if n == 0 {
		return nil, nil
	}

	output = make([]byte, 0, n)

	var head [hashSize]int32
	var prev [WindowSize]int32
	for i := range head {
		head[i] = -1
	}

	maxChain := 32
	niceMatch := 32 // length at which we stop searching the chain
	if exhaustive {
		maxChain = WindowSize
		niceMatch = MaxCoded - 1
	}

	findMatch := func(i, minLen int) (bestPos, bestLen int) {
		bestLen = minLen
		bestPos = -1

		if i+MaxUncoded > n {
			return
		}
		maxLen := MaxCoded - 1
		if n-i < maxLen {
			maxLen = n - i
		}
		if maxLen <= bestLen {
			return
		}

		h := hash3(data[i], data[i+1], data[i+2])
		minPos := int32(i - WindowSize + 1)
		if minPos < 0 {
			minPos = 0
		}

		cur := head[h]
		for chain := maxChain; cur >= minPos && chain > 0; chain-- {
			j := int(cur)

			if bestLen >= maxLen {
				return
			}

			// quick reject
			if bestLen >= MaxUncoded && data[j+bestLen] != data[i+bestLen] {
				cur = prev[j&(WindowSize-1)]
				continue
			}
			// check collision
			if data[j] != data[i] || data[j+1] != data[i+1] || data[j+2] != data[i+2] {
				cur = prev[j&(WindowSize-1)]
				continue
			}

			k := 3
			for k < maxLen && data[j+k] == data[i+k] {
				k++
			}
			if k > bestLen {
				bestLen = k
				bestPos = j
				if k >= niceMatch {
					return
				}
			}
			cur = prev[j&(WindowSize-1)]
		}
		return
	}

	insertHash := func(pos int) {
		if pos+MaxUncoded > n {
			return
		}
		h := hash3(data[pos], data[pos+1], data[pos+2])
		prev[pos&(WindowSize-1)] = head[h]
		head[h] = int32(pos)
	}

	if exhaustive {
		return compressExhaustive(data, output, findMatch, insertHash, n), nil
	}

	var flagsOffset int
	var flagsBit uint

	i := 0
	for i < n {
		if flagsBit == 0 {
			flagsOffset = len(output)
			output = append(output, 0)
		}

		matchPos, matchLen := findMatch(i, MaxUncoded-1)

		// lazy match
		if matchLen >= MaxUncoded && matchLen < MaxCoded-1 && i+1 < n {
			_, nextLen := findMatch(i+1, matchLen)
			if nextLen > matchLen {
				output[flagsOffset] |= 1 << flagsBit
				output = append(output, data[i])
				insertHash(i)
				i++
				flagsBit = (flagsBit + 1) & 7
				continue
			}
		}

		if matchLen >= MaxUncoded {
			wp := (WindowInitialIndex + matchPos) & (WindowSize - 1)
			output = append(output, byte(wp))
			output = append(output, byte((wp>>LengthBits)&0xF0)|byte((matchLen-MaxUncoded)&0x0F))
			for k := 0; k < matchLen; k++ {
				insertHash(i + k)
			}
			i += matchLen
		} else {
			output[flagsOffset] |= 1 << flagsBit
			output = append(output, data[i])
			insertHash(i)
			i++
		}

		flagsBit = (flagsBit + 1) & 7
	}

	return output, nil
}

func compressExhaustive(data []byte, output []byte, findMatch func(i, minLen int) (int, int), insertHash func(pos int), n int) []byte {
	matchPos := make([]int32, n)
	matchLen := make([]uint8, n)

	// find longest match per position
	for i := 0; i < n; i++ {
		pos, length := findMatch(i, MaxUncoded-1)
		if length >= MaxUncoded {
			matchPos[i] = int32(pos)
			matchLen[i] = uint8(length)
		}
		insertHash(i)
	}

	// backwards cost check
	const literalCost = 8 + 1
	const matchCost = 16 + 1
	cost := make([]uint32, n+1)
	step := make([]uint8, n)
	for i := n - 1; i >= 0; i-- {
		bestCost := cost[i+1] + literalCost
		bestStep := uint8(1)
		if mlen := int(matchLen[i]); mlen >= MaxUncoded {
			for L := MaxUncoded; L <= mlen; L++ {
				c := cost[i+L] + matchCost
				if c < bestCost {
					bestCost = c
					bestStep = uint8(L)
				}
			}
		}
		cost[i] = bestCost
		step[i] = bestStep
	}

	// output with above costs
	var flagsOffset int
	var flagsBit uint
	for i := 0; i < n; {
		if flagsBit == 0 {
			flagsOffset = len(output)
			output = append(output, 0)
		}
		s := int(step[i])
		if s == 1 {
			output[flagsOffset] |= 1 << flagsBit
			output = append(output, data[i])
			i++
		} else {
			wp := (WindowInitialIndex + int(matchPos[i])) & (WindowSize - 1)
			output = append(output, byte(wp))
			output = append(output, byte((wp>>LengthBits)&0xF0)|byte((s-MaxUncoded)&0x0F))
			i += s
		}
		flagsBit = (flagsBit + 1) & 7
	}
	return output
}

const (
	hashBits = 13
	hashSize = 1 << hashBits
)

func hash3(a, b, c byte) uint32 {
	return ((uint32(a)<<16 | uint32(b)<<8 | uint32(c)) * 2654435761) >> (32 - hashBits)
}
