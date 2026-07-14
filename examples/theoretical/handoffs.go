package theoretical

import "example.com/extlib"

// WarnChanOverChan hands channels between goroutines through a channel of
// channels (the pipechan pattern) — the consumer is whoever receives the
// inner channel, invisible to local analysis: LEAK WARN, not LEAK.
func WarnChanOverChan(inputs <-chan int) {
	channels := make(chan chan int, 2)
	go func() {
		defer close(channels)
		curr := make(chan int, 4)
		channels <- curr
		for v := range inputs {
			curr <- v
		}
		close(curr)
	}()
	go func() {
		for curr := range channels {
			for v := range curr { // fed by the sibling goroutine's handoff
				_ = v
			}
		}
	}()
}

// SafeLibChanMultiReturn receives from a channel obtained via the common
// (channel, error) library shape — the library owns the other end. Must
// produce no leak finding.
func SafeLibChanMultiReturn() int {
	resultCh, err := extlib.StreamErr(4)
	if err != nil {
		return 0
	}
	total := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for v := range resultCh {
			total += v
		}
	}()
	<-done
	return total
}

type perfResult struct{ Bytes int }

type wrapped struct{ Result *perfResult }

func render(r *perfResult) { _ = r }

func consume(w wrapped) { _ = w }

// WarnLoopVarAddrEscape reuses one variable across channel iterations and
// hands out its address every time — the receiver can observe later
// overwrites: RACE WARN.
func WarnLoopVarAddrEscape(results <-chan perfResult) {
	var result perfResult
	for result = range results {
		render(&result)
	}
}

// WarnLoopVarAddrInStruct is the support-perf-object shape: the reused
// variable's address escapes nested inside a struct literal passed to a
// UI/collector — RACE WARN.
func WarnLoopVarAddrInStruct(results <-chan perfResult) {
	var result perfResult
	for result = range results {
		consume(wrapped{Result: &result})
	}
}

// SafeLoopVarPerIteration declares the variable per iteration — each
// address is distinct. Must produce no finding.
func SafeLoopVarPerIteration(results <-chan perfResult) {
	for result := range results {
		render(&result)
	}
}
