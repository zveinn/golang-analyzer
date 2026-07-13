package main

type Number interface {
	~int | ~int64 | ~float64
}

// Sum is generic; call sites should show the inferred type arguments.
func Sum[T Number](a, b T) T {
	return add(a, b)
}

func add[T Number](a, b T) T {
	return a + b
}

func Map[T, U any](in []T, fn func(T) U) []U {
	out := make([]U, 0, len(in))
	for _, v := range in {
		out = append(out, fn(v))
	}
	return out
}

func UseGenerics() []string {
	total := Sum(int64(1), int64(2))
	labels := Map([]int64{total}, formatID)
	return labels
}

func formatID(v int64) string {
	return "id-" + string(rune('0'+v%10))
}
