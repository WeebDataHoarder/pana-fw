package utils

func Align[T ~int | ~uint32 | ~uint64](n, align T) T {
	return (n + align - 1) / align * align
}
