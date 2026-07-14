// Package extlib simulates an external dependency so the analyzer's
// [module] classification can be tested without network access.
package extlib

import "fmt"

func Validate(name string) error {
	if name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

// Stream mimics a library API returning a channel it owns (like minio-go's
// ListObjects) — the library side sends and closes.
func Stream(n int) <-chan int {
	ch := make(chan int, n)
	go func() {
		defer close(ch)
		for i := 0; i < n; i++ {
			ch <- i
		}
	}()
	return ch
}

// StreamErr mimics madmin-style APIs returning (channel, error) — the
// library goroutine feeds and closes the channel.
func StreamErr(n int) (<-chan int, error) {
	if n < 0 {
		return nil, fmt.Errorf("negative count")
	}
	ch := make(chan int, n)
	go func() {
		defer close(ch)
		for i := 0; i < n; i++ {
			ch <- i
		}
	}()
	return ch, nil
}

func Transform(data []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ 0x5a
	}
	return out
}
