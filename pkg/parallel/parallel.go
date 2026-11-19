// Package parallel runs independent work across CPUs.
package parallel

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// ForEach calls fn(i) for every i in [0, n) on up to GOMAXPROCS goroutines;
// the returned wait re-raises a panic from fn on the caller's goroutine.
func ForEach(n int, fn func(i int)) (wait func()) {
	var (
		next     atomic.Int64
		panicked atomic.Pointer[any]
		wg       sync.WaitGroup
	)
	for range min(runtime.GOMAXPROCS(0), n) {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					panicked.CompareAndSwap(nil, &r)
				}
			}()
			for i := int(next.Add(1)) - 1; i < n; i = int(next.Add(1)) - 1 {
				fn(i)
			}
		})
	}
	return func() {
		wg.Wait()
		if r := panicked.Load(); r != nil {
			panic(*r)
		}
	}
}

// Go runs fn on its own goroutine; wait blocks until fn returns and re-raises its panic.
func Go(fn func()) (wait func()) {
	return ForEach(1, func(int) { fn() })
}
