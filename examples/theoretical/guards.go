package theoretical

import (
	"sync"
	"sync/atomic"
)

// WarnMutexGuarded serializes every access with the same mutex — safe, but
// lock pairing is beyond static verification, so it grades as a warning.
func WarnMutexGuarded(n int) int {
	total := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			total++
			mu.Unlock()
		}()
	}
	// unsynchronized-looking read, but under the same lock
	mu.Lock()
	snapshot := total
	mu.Unlock()
	wg.Wait()
	return snapshot
}

// WarnRWMutexGuarded writes under Lock in the goroutine and reads under
// RLock outside.
func WarnRWMutexGuarded(seed int) int {
	state := 0
	var mu sync.RWMutex
	done := make(chan struct{})
	go func() {
		mu.Lock()
		state = seed * 3
		mu.Unlock()
		close(done)
	}()
	mu.RLock()
	v := state
	mu.RUnlock()
	<-done
	return v
}

// WarnOnceGuarded initializes the shared variable inside sync.Once.Do from
// several goroutines — the write executes at most once.
func WarnOnceGuarded(paths []string) *[]string {
	var cache *[]string
	var once sync.Once
	var wg sync.WaitGroup
	for range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			once.Do(func() {
				c := make([]string, 0, len(paths))
				cache = &c
			})
		}()
	}
	wg.Wait()
	return cache
}

// SafeAtomicFuncs mediates every access through sync/atomic functions —
// fully synchronized, must produce no finding at all.
func SafeAtomicFuncs(n int) int64 {
	var counter int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt64(&counter, 1)
		}()
	}
	wg.Wait()
	return atomic.LoadInt64(&counter)
}

// SafeChanJoined writes in the goroutine and reads only after receiving
// the goroutine's completion signal — a happens-before edge through the
// channel. Must produce no finding.
func SafeChanJoined(n int) int {
	result := 0
	done := make(chan struct{})
	go func() {
		result = n * n
		close(done)
	}()
	<-done
	return result
}

// ConcreteMixedAtomic writes atomically inside but reads plainly outside
// without any join — a real race (mixed atomic/non-atomic access).
func ConcreteMixedAtomic(n int) int64 {
	var counter int64
	for i := 0; i < n; i++ {
		go func() {
			atomic.AddInt64(&counter, 1)
		}()
	}
	return counter // plain read, unsynchronized
}
