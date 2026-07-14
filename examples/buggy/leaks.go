package buggy

import "context"

// LeakBlockedSend launches a goroutine that sends on a channel nobody ever
// reads — it blocks forever.
func LeakBlockedSend(n int) {
	results := make(chan int)
	go func() {
		results <- n * n // no readers anywhere
	}()
	// ... forgot to read results
}

// LeakBlockedRecv waits on a channel that is never written or closed.
func LeakBlockedRecv() {
	done := make(chan struct{})
	go func() {
		<-done // no writers, no close
	}()
}

// LeakBusyLoop spins forever with no exit condition.
func LeakBusyLoop(counters []int) {
	go func() {
		i := 0
		for {
			i++ // no return, no break, no channel wait
		}
	}()
	_ = counters
}

// WorkerOK drains its input and honors cancellation — must NOT be flagged.
func WorkerOK(ctx context.Context, jobs <-chan int, results chan<- int) {
	go func() {
		for {
			select {
			case j, ok := <-jobs:
				if !ok {
					return
				}
				results <- j * 2
			case <-ctx.Done():
				return
			}
		}
	}()
}

func feedWorker(ctx context.Context) []int {
	jobs := make(chan int, 3)
	results := make(chan int, 3)
	WorkerOK(ctx, jobs, results)
	jobs <- 1
	jobs <- 2
	close(jobs)
	return []int{<-results, <-results}
}
