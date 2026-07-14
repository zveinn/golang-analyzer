package theoretical

import "sync"

type scanBuf struct {
	err   error
	fill  chan int
	items []int
}

// SafePointerValueRead mirrors jstream.newScanner: the goroutine writes
// fields through the shared pointer; the parent only copies the POINTER
// VALUE (`return sb`) — which touches none of the pointee's memory. Must
// produce no finding.
func SafePointerValueRead(n int) *scanBuf {
	sb := &scanBuf{fill: make(chan int, 1)}
	go func() {
		sb.err = nil
		sb.items = make([]int, n)
		sb.fill <- n
	}()
	<-sb.fill
	return sb
}

// WarnSiblingGoroutineMutex mirrors NSScanner: the writer goroutine and a
// sibling reader goroutine both hold the same mutex — theoretical, WARN.
func WarnSiblingGoroutineMutex(n int) []int {
	results := make([]int, 4)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		mu.Lock()
		results[0] = n
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		mu.Lock()
		snapshot := make([]int, len(results))
		copy(snapshot, results)
		mu.Unlock()
		_ = snapshot
	}()
	wg.Wait()
	return results
}

type presigner struct {
	dst []string
}

func (p *presigner) name(i int) string { return string(rune('a' + i)) }

// WarnShardWithMethodCalls mirrors delta-sharing-presign: sharded element
// writes plus method calls on the shared receiver (unverifiable, so WARN
// rather than RACE).
func WarnShardWithMethodCalls(p *presigner, n int) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p.dst[i] = p.name(i)
		}(i)
	}
	wg.Wait()
}
