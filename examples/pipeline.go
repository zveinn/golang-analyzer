package main

// generator/pipeline patterns: channels created locally, closed by the
// producer, ranged over by the consumer.

func RunPipeline(limit int) int {
	nums := generate(limit)
	squared := square(nums)
	total := 0
	for v := range squared {
		total += v
	}
	return total
}

func generate(limit int) <-chan int {
	out := make(chan int)
	go func() {
		for i := 0; i < limit; i++ {
			out <- i
		}
		close(out)
	}()
	return out
}

func square(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		for v := range in {
			out <- v * v
		}
		close(out)
	}()
	return out
}
