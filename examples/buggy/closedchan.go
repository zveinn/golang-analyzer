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
