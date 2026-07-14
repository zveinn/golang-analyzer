package theoretical

import "sync"

// WarnExclusiveBranches mirrors CopyAligned: the goroutine only exists in
// the buffered branch, and the conflicting write only happens in the
// unbuffered one — correlated conditions that never overlap today.
func WarnExclusiveBranches(size int, useBuffer bool) int {
	reader := size
	var wg sync.WaitGroup
	if useBuffer {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reader * 2 // reads the shared variable
		}()
	}
	if !useBuffer {
		reader = size + 1 // only when the goroutine was never launched
		return reader
	}
	wg.Wait()
	return 0
}

// WarnSwitchExclusive is the same shape through switch cases.
func WarnSwitchExclusive(mode int) int {
	buf := 0
	var wg sync.WaitGroup
	switch mode {
	case 1:
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf = 10
		}()
		wg.Wait()
	case 2:
		buf = 20 // different case — never concurrent with case 1's goroutine
	}
	return buf
}

// warnUnreachable contains a textbook race, but no caller exists in this
// codebase — theoretical until someone wires it up.
func warnUnreachable(n int) int { //nolint:unused // deliberately uncalled
	x := 0
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x++
		}()
	}
	wg.Wait()
	return x
}

// SafeWriteBeforeLaunch fully initializes the variable before the
// goroutine starts and never touches it again — reads on both sides only,
// no finding.
func SafeWriteBeforeLaunch(n int) chan int {
	config := n * 42
	out := make(chan int, 1)
	go func() {
		out <- config
	}()
	return out
}
