package testfactory

func Repeat[T any](s T, count int) []T {
	ss := make([]T, count)
	for i := 0; i < count; i++ {
		ss[i] = s
	}
	return ss
}
