// Package theoretical is a corpus of "theoretical races": code shapes a
// syntactic race detector pairs up, but which cannot affect any caller in
// the current codebase. The scanner must grade every WarnXxx function as
// RACE WARN, keep every ConcreteXxx as RACE, and stay silent on SafeXxx.
package theoretical

import "sync"

// WarnShardRangeKey fans out one goroutine per index; each instance writes
// only its own slice element (captured per-iteration range key).
func WarnShardRangeKey(inputs []int) []int {
	results := make([]int, len(inputs))
	var wg sync.WaitGroup
	for i := range inputs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = inputs[i] * 2
		}()
	}
	wg.Wait()
	return results
}

// WarnShardParam passes the index as a goroutine parameter — the classic
// pre-Go-1.22 sharding style.
func WarnShardParam(inputs []string) []int {
	lengths := make([]int, len(inputs))
	var wg sync.WaitGroup
	for i := 0; i < len(inputs); i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			lengths[idx] = len(inputs[idx])
		}(i)
	}
	wg.Wait()
	return lengths
}

// WarnShardPointer mirrors dsync's releaseAll: the goroutines share a
// *[]string and write disjoint elements through it.
func WarnShardPointer(locks *[]string, n int) {
	var wg sync.WaitGroup
	for lockID := 0; lockID < n; lockID++ {
		wg.Add(1)
		go func(lockID int) {
			defer wg.Done()
			if (*locks)[lockID] != "" {
				(*locks)[lockID] = ""
			}
		}(lockID)
	}
	wg.Wait()
}

// WarnShardChunks splits the slice into ranges; every index a goroutine
// touches is derived from its private chunk bounds.
func WarnShardChunks(data []float64, workers int) {
	var wg sync.WaitGroup
	chunk := (len(data) + workers - 1) / workers
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for j := start; j < start+chunk && j < len(data); j++ {
				data[j] *= 0.5
			}
		}(w * chunk)
	}
	wg.Wait()
}

// WarnShardNested writes rows of a matrix — the first index is
// per-instance, so rows are disjoint.
func WarnShardNested(rows [][]int) {
	var wg sync.WaitGroup
	for r := range rows {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range rows[r] {
				rows[r][c]++
			}
		}()
	}
	wg.Wait()
}

// ConcreteShardWholeRead shards its writes but iterates the whole slice
// while the goroutines are still running — a real race.
func ConcreteShardWholeRead(inputs []int) int {
	results := make([]int, len(inputs))
	for i := range inputs {
		go func() {
			results[i] = inputs[i] * 2
		}()
	}
	total := 0
	for _, v := range results { // no join — reads every element
		total += v
	}
	return total
}

// ConcreteShardMap uses per-instance keys, but concurrent map writes are
// never safe regardless of key disjointness.
func ConcreteShardMap(inputs []int) map[int]int {
	out := map[int]int{}
	var wg sync.WaitGroup
	for i, v := range inputs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = v * 2 // concurrent map write — panics
		}()
	}
	wg.Wait()
	return out
}
