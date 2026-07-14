package theoretical

// WarnLeakParamChannel blocks on a channel received as a parameter — the
// counterpart belongs to the caller (the pubsub.Subscribe doneCh pattern),
// so this is unverifiable rather than a proven leak: LEAK WARN.
func WarnLeakParamChannel(done <-chan struct{}, hook func()) {
	go func() {
		<-done
		hook()
	}()
}

// WarnLeakBufferedSend sends to a buffered channel with no visible reader —
// the send only blocks once the buffer fills: LEAK WARN.
func WarnLeakBufferedSend(n int) {
	resp := make(chan int, 1)
	go func() {
		resp <- n
	}()
}
