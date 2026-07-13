package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"example.com/extlib"
)

type Config struct {
	Workers int
	Name    string
}

type Pool struct {
	jobs    chan int
	results chan int
	done    chan struct{}
	count   atomic.Int64
}

func main() {
	cfg := &Config{Workers: 4, Name: "demo"}
	if err := extlib.Validate(cfg.Name); err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	Run(ctx, cfg)
}

// Run spins up a worker pool, feeds it, and collects results.
func Run(ctx context.Context, cfg *Config) {
	pool := NewPool(cfg.Workers)
	var wg sync.WaitGroup

	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go pool.worker(ctx, &wg, i)
	}

	go produce(pool.jobs, 100)

	go func() {
		wg.Wait()
		close(pool.results)
	}()

	total := collect(pool.results)
	report(cfg, total, pool.count.Load())
}

func NewPool(n int) *Pool {
	return &Pool{
		jobs:    make(chan int, n*2),
		results: make(chan int, n*2),
		done:    make(chan struct{}),
	}
}

func (p *Pool) worker(ctx context.Context, wg *sync.WaitGroup, id int) {
	defer wg.Done()
	for {
		select {
		case job, ok := <-p.jobs:
			if !ok {
				return
			}
			p.results <- compute(job)
			p.count.Add(1)
		case <-ctx.Done():
			return
		case <-p.done:
			return
		}
	}
}

func produce(jobs chan<- int, n int) {
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
}

func collect(results <-chan int) int {
	total := 0
	for r := range results {
		total += Sum(r, total)
	}
	return total
}

func compute(job int) int {
	data := extlib.Transform([]byte{byte(job)})
	return job*job + int(data[0])
}

func report(cfg *Config, total int, processed int64) {
	fmt.Printf("%s: total=%d processed=%d\n", cfg.Name, total, processed)
}
