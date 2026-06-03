package merkle

import "sync"

// hashGrain is the per-worker item floor for the cheapest parallelized work, a
// single node hash. Benchmarked: below a few hundred such hashes sequential wins,
// and the fan-out cost grows with the worker count, so over-decomposing hurts most.
const hashGrain = 256

// splitWorkers reports how many workers to use for count items: at most workers,
// and enough that each handles about grain items — or 1 (run sequentially) when
// the work is too small to split. grain balances per-item cost against fan-out
// overhead: cheap items (one hash) want a large grain, pricey ones (a whole proof)
// a smaller one.
func splitWorkers(count, workers, grain int) int {
	if workers <= 1 {
		return 1
	}
	return min(workers, max(1, count/grain))
}

// parallelize runs fn(i) for every i in [0,count), fanning out across workers
// goroutines when the work is large enough to split (see [splitWorkers]).
func parallelize(count, workers int, fn func(i int)) {
	workers = splitWorkers(count, workers, hashGrain)
	if workers <= 1 {
		for i := range count {
			fn(i)
		}
		return
	}
	parallelChunks(count, workers, func(_, start, end int) {
		for i := start; i < end; i++ {
			fn(i)
		}
	})
}

// hashLeaves runs leaf+hash over [0,leaves), recycling each leaf's returned slice
// as the next dst (see [NewTreeFuncInto]). Each worker threads its own dst from
// nil, so workers never share scratch. It splits only when worthwhile (see
// [splitWorkers]).
func hashLeaves(leaves int, workers int, leaf func(i int, dst []byte) []byte, hash func(i int, b []byte)) {
	workers = splitWorkers(leaves, workers, hashGrain)
	if workers <= 1 {
		var dst []byte
		for i := range leaves {
			s := leaf(i, dst)
			hash(i, s)
			dst = s
		}
		return
	}
	parallelChunks(leaves, workers, func(_, start, end int) {
		var dst []byte
		for i := start; i < end; i++ {
			s := leaf(i, dst)
			hash(i, s)
			dst = s
		}
	})
}

// parallelChunks splits [0,count) into workers contiguous chunks and runs
// fn(w, start, end) for each non-empty chunk in its own goroutine, where w is the
// chunk ordinal. Callers pass an already-capped worker count (see [splitWorkers]).
// It blocks until all complete and returns the chunk count, which lets callers
// size per-chunk result storage.
func parallelChunks(count, workers int, fn func(w, start, end int)) int {
	workers = min(workers, count)
	chunk := (count + workers - 1) / workers
	chunks := (count + chunk - 1) / chunk // ≤ workers, every chunk non-empty

	var wg sync.WaitGroup
	wg.Add(chunks)
	for w := range chunks {
		start, end := w*chunk, min((w+1)*chunk, count)
		go func() {
			defer wg.Done()
			fn(w, start, end)
		}()
	}
	wg.Wait()
	return chunks
}
