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

// BranchCorrelated mirrors the mutually-exclusive-branches pattern: the
// goroutine only exists when n > 10, and the conflicting write runs only
// when n <= 10. This cannot race today — it should be flagged as a
// RACE WARN (racy only after a code change), not a full RACE.
func BranchCorrelated(n int) int {
	result := 0
	if n > 10 {
		go func() {
			result = n * 2 // write inside the goroutine
		}()
	}
	if n <= 10 {
		result = 5 // mutually exclusive with the goroutine's branch
		return result
	}
	return 0
}

// WaitGroupSynced writes from a single goroutine and reads only after
// joining it — fully synchronized, must NOT be flagged at all.
func WaitGroupSynced(n int) int {
	result := 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		result = n * 2
	}()
	wg.Wait()
	return result
}

// unreachableRace contains a textbook race, but nothing in this codebase
// calls it — the race is theoretical until someone wires it up, so it must
// be flagged as RACE WARN, not RACE.
func unreachableRace(n int) int { //nolint:unused // deliberately uncalled
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
