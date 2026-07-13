package main

// direct and mutual recursion — the tracer must not loop forever.

func Fib(n int) int {
	if n < 2 {
		return n
	}
	return Fib(n-1) + Fib(n-2)
}

func IsEven(n int) bool {
	if n == 0 {
		return true
	}
	return isOdd(n - 1)
}

func isOdd(n int) bool {
	if n == 0 {
		return false
	}
	return IsEven(n - 1)
}

// UseFuncValues exercises calls through function-typed variables.
func UseFuncValues(n int) int {
	double := func(x int) int { return x * 2 }
	op := Fib
	return double(op(n))
}
