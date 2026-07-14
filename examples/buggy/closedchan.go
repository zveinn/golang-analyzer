package buggy

import "sync"

// SendAfterClose sends on a channel that was already closed in the same
// flow — this panics at runtime.
func SendAfterClose() {
	ch := make(chan int, 4)
	ch <- 1
	close(ch)
	ch <- 2 // send on closed channel
}

// UncoordinatedClose closes a channel while independent goroutines are
// still sending to it — any late sender panics.
func UncoordinatedClose(n int) <-chan int {
	out := make(chan int)
	for i := 0; i < n; i++ {
		go func(v int) {
			out <- v * v
		}(i)
	}
	go func() {
		close(out) // nothing guarantees the senders are done
	}()
	return out
}

// CoordinatedCloseOK waits for all senders before closing — must NOT be
// flagged.
func CoordinatedCloseOK(n int) <-chan int {
	out := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			out <- v * v
		}(i)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

// BranchExclusiveClose closes and sends in mutually exclusive branches —
// cannot panic today, so it must be graded CLOSED CH WARN (theoretical),
// not a full finding.
func BranchExclusiveClose(flag bool) {
	ch := make(chan int, 1)
	if flag {
		close(ch)
	} else {
		ch <- 1
	}
}

// unreachableSendAfterClose is a textbook panic, but nothing in this
// codebase calls it — graded CLOSED CH WARN until someone wires it up.
func unreachableSendAfterClose() { //nolint:unused // deliberately uncalled
	ch := make(chan int, 1)
	close(ch)
	ch <- 1
}

func drainSquares(n int) int {
	total := 0
	for v := range CoordinatedCloseOK(n) {
		total += v
	}
	for v := range UncoordinatedClose(n) {
		total += v
	}
	return total
}
