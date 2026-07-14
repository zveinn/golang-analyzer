// Package buggy exercises the repo scanner: every function here contains
// (or deliberately avoids) a defect the scanner should classify.
package buggy

import (
	"sync"
	"sync/atomic"
)

// RaceCounter increments a captured counter from many goroutines — a
// classic data race the scanner must flag.
func RaceCounter(n int) int {
	counter := 0
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter++ // racy write
		}()
	}
	wg.Wait()
	return counter
}

// RaceAppend grows a shared slice from goroutines without locking.
func RaceAppend(items []string) []int {
	var lengths []int
	var wg sync.WaitGroup
	for _, it := range items {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lengths = append(lengths, len(it)) // racy write
		}()
	}
	wg.Wait()
	return lengths
}

// AtomicCounterOK does the same work with sync/atomic — must NOT be
// flagged as a race.
func AtomicCounterOK(n int) int64 {
	var counter atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter.Add(1)
		}()
	}
	wg.Wait()
	return counter.Load()
}
