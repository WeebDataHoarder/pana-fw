package lzss

type Window [WindowSize]byte

func CreateWindow() (t Window) {
	_ = t[WindowInitialIndex-1]

	return t
}

func (t *Window) GetSet(offsetIndex, windowIndex, length int, buf []byte) (int, []byte) {
	_ = buf[length-1]

	for j := 0; j < length; j++ {
		buf[j] = t[t.Index(offsetIndex+j)]
		t[t.Index(windowIndex+j)] = buf[j]
	}

	return t.Index(windowIndex + length), buf[:length]
}

func (t *Window) SetByte(windowIndex int, data byte) int {
	t[t.Index(windowIndex)] = data
	return t.Index(windowIndex + 1)
}

func (t *Window) Index(i int) int {
	return i & (WindowSize - 1)
}
