package utils

func Align[T ~int | ~uint32](n, align T) T {
	return ((n-1)/align + 1) * align
}
