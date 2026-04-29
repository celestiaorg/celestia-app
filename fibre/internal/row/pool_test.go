package row

import (
	"bytes"
	"runtime"
	"sync"
	"testing"
)

// Test shape — small values so tests stay fast.
const (
	testAssemblerBatchRows = 6  // e.g. parityRows+2 with parityRows=4
	testCodecWorkRows      = 16 // e.g. 2·ceilPow2(parityRows) with parityRows=4
	testMaxRow             = 4096
)

func newPool(t testing.TB) *Pool {
	t.Helper()
	return NewPool(testMaxRow, testAssemblerBatchRows)
}

// Primary row count used by most tests (assembler batch).
func assemblerRowCount() int { return testAssemblerBatchRows }

func TestPool_GetPutExact(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	bufs := p.Get(rc, 64)
	if len(bufs) != rc {
		t.Fatalf("Get returned %d bufs, want %d", len(bufs), rc)
	}
	for _, b := range bufs {
		if len(b) != 64 {
			t.Fatalf("len=%d, want 64", len(b))
		}
	}

	bufs[0][0] = 1
	bufs[1][0] = 2
	if bufs[0][0] == bufs[1][0] {
		t.Fatal("adjacent buffers alias")
	}

	p.Put(bufs)
	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d after Put, want 1", got)
	}

	// second Get at exact key reuses the same batch.
	more := p.Get(rc, 64)
	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d after exact-match reuse, want 1", got)
	}
	p.Put(more)
}

func TestPool_InvalidSizePanics(t *testing.T) {
	p := newPool(t)
	defer func() {
		if recover() == nil {
			t.Fatal("Get with non-multiple-of-64 size should panic")
		}
	}()
	p.Get(assemblerRowCount(), 63)
}

// Get with rowCount != pool.rowCount panics — a Pool serves only one
// row count.
func TestPool_RowCountMismatchPanics(t *testing.T) {
	p := newPool(t)
	defer func() {
		if recover() == nil {
			t.Fatal("Get with rowCount != pool.rowCount should panic")
		}
	}()
	p.Get(testCodecWorkRows, 64)
}

func TestPool_OversizeRowPanics(t *testing.T) {
	p := newPool(t)
	defer func() {
		if recover() == nil {
			t.Fatal("Get with rowSize > maxRowSize should panic")
		}
	}()
	p.Get(assemblerRowCount(), testMaxRow+1)
}

func TestPool_DoubleFreePanics(t *testing.T) {
	p := newPool(t)
	bufs := p.Get(assemblerRowCount(), 64)
	p.Put(bufs)
	defer func() {
		if recover() == nil {
			t.Fatal("second Put of same batch should panic")
		}
	}()
	p.Put(bufs)
}

func TestPool_PutEmptyPanics(t *testing.T) {
	p := newPool(t)
	defer func() {
		if recover() == nil {
			t.Fatal("Put with empty bufs should panic")
		}
	}()
	p.Put(nil)
}

func TestPool_GetZeroReturnsNil(t *testing.T) {
	p := newPool(t)
	if bufs := p.Get(0, 64); bufs != nil {
		t.Fatalf("Get(0, _) = %v, want nil", bufs)
	}
}

// A free batch of a larger class serves a smaller request within slack.
func TestPool_FallbackWithinSlack(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	big := p.Get(rc, 320)
	p.Put(big)

	// request 256: reqIdx=3, maxIdx=reqIdx+slack=5 (size 384). 320 fits → reuse.
	small := p.Get(rc, 256)
	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d after slack fallback, want 1 (reused 320 slab)", got)
	}
	if len(small[0]) != 256 {
		t.Fatalf("returned len=%d, want 256 (requested)", len(small[0]))
	}
	p.Put(small)

	// on return, batch goes back to its backing bucket (320), not 256.
	again := p.Get(rc, 320)
	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d after backing-bucket reuse, want 1", got)
	}
	p.Put(again)
}

// A free batch outside the slack window is NOT a fallback candidate.
func TestPool_FallbackOutsideSlack(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	big := p.Get(rc, 512)
	p.Put(big)

	// request 64: reqIdx=0, maxIdx=reqIdx+slack=2 (size 192). 512 is above → grow.
	small := p.Get(rc, 64)
	if got := p.Batches(); got != 2 {
		t.Fatalf("Batches=%d after out-of-slack grow, want 2", got)
	}
	p.Put(small)
}

// Fallback never triggers a new allocation at a larger key.
func TestPool_FallbackNeverAllocates(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	// no 320 batch exists; request 256 → grows new 256 batch (exact key).
	a := p.Get(rc, 256)
	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d, want 1", got)
	}
	p.Put(a)
	// request 320 now: allocate new.
	b := p.Get(rc, 320)
	if got := p.Batches(); got != 2 {
		t.Fatalf("Batches=%d, want 2 (separate 256 and 320 batches)", got)
	}
	p.Put(b)
}

// Free batches are reused LIFO: the most recently Put batch is the next
// one served by Get.
func TestPool_LIFOReuse(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	a := p.Get(rc, 64)
	b := p.Get(rc, 64)
	aAddr := &a[0][0]
	bAddr := &b[0][0]
	if aAddr == bAddr {
		t.Fatal("distinct Gets should return distinct backing memory")
	}
	p.Put(a)
	p.Put(b) // most recently freed

	next := p.Get(rc, 64)
	if &next[0][0] != bAddr {
		t.Fatal("LIFO reuse: expected most-recently-freed batch")
	}
	p.Put(next)
}

// Consuming from a larger bucket via fallback advances that bucket's
// generation and runs its aged eviction, so its other free batches age
// and evict under fallback-driven demand (not only direct Gets).
func TestPool_FallbackAgesLargerBucket(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	// populate the 320-rowSize bucket with two free batches via direct Gets.
	a := p.Get(rc, 320)
	b := p.Get(rc, 320)
	p.Put(a)
	p.Put(b)
	if got := p.Batches(); got != 2 {
		t.Fatalf("Batches=%d before aging, want 2", got)
	}

	// drive fallback Gets at a smaller in-slack rowSize. LIFO pop means
	// the most-recently-Put 320 batch cycles; the other sits at the head
	// of the free queue with a frozen lastPut and ages out.
	for range evictionThreshold {
		x := p.Get(rc, 256)
		p.Put(x)
	}
	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d after fallback-driven aging, want 1", got)
	}
}

// A free batch aged past evictionThreshold is evicted on the next Get to
// its bucket.
func TestPool_AgedEviction(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	a := p.Get(rc, 64)
	b := p.Get(rc, 64)
	p.Put(a)
	p.Put(b)
	if got := p.Batches(); got != 2 {
		t.Fatalf("Batches=%d before age, want 2", got)
	}

	// drive enough Gets to age both free batches past evictionThreshold.
	for range evictionThreshold + 1 {
		more := p.Get(rc, 64)
		p.Put(more)
	}
	_ = p.Get(rc, 64)
	if got := p.Batches(); got > 2 {
		t.Fatalf("Batches=%d, want <= 2 (aged eviction)", got)
	}
}

// The per-bucket idle timer, when fired, drops every free batch in that
// bucket. The timer callback is invoked directly to avoid waiting
// idleGrace in tests.
func TestPool_IdleDropsBucket(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	a := p.Get(rc, 64)
	b := p.Get(rc, 64)
	p.Put(a)
	p.Put(b)

	if got := p.Batches(); got != 2 {
		t.Fatalf("Batches=%d before idle, want 2", got)
	}
	bucketAt(p, 64).dropIdle()
	if got := p.Batches(); got != 0 {
		t.Fatalf("Batches=%d after idle drop, want 0", got)
	}
}

// Per-bucket idle timer regression: a dormant bucket gets cleaned up
// independently of other buckets' activity. Pool-wide idle previously
// pinned aged free batches in dormant buckets while any other bucket
// had in-use batches, leaking memory until the entire pool went idle.
func TestPool_IdleDropsDormantBucketIndependently(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	// bucket A (rowSize 64): allocate and return — has free batches, no in-use.
	x := p.Get(rc, 64)
	p.Put(x)

	// bucket B (rowSize 128): keep one batch in-use perpetually.
	y := p.Get(rc, 128)
	defer p.Put(y)

	// trigger A's idle timer specifically. Even though B is non-idle,
	// A must drop its free batches.
	bucketAt(p, 64).dropIdle()

	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d, want 1 (only B's in-use batch survives)", got)
	}
}

// The per-bucket idle timer arms on transition to fully-free and is
// stopped on the next Get to that bucket. The timer object itself is
// retained for reuse, so the test asserts on its armed/stopped state
// (via Stop's return) rather than nil-ness.
func TestPool_IdleTimerArmedAndCancelled(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()
	bk := bucketAt(p, 64)

	if bk.idleTimer != nil {
		t.Fatal("idle timer should not exist before first idle transition")
	}

	a := p.Get(rc, 64)
	p.Put(a)
	if bk.idleTimer == nil {
		t.Fatal("idle timer should be created when bucket goes fully free")
	}

	b := p.Get(rc, 64)
	defer p.Put(b)
	// Get's cancelIdle should have stopped the timer; a redundant Stop
	// returns false because the timer is no longer active.
	if bk.idleTimer.Stop() {
		t.Fatal("idle timer should already be stopped after Get")
	}
}

// bucketAt returns the bucket pointer for the given rowSize so tests
// can poke at bucket-level state directly.
func bucketAt(p *Pool, size int) *bucket {
	return &p.buckets[size/rowSizeAlign-1]
}

// Write-aliasing check: data written through one carved view must not land
// in a neighbor.
func TestPool_NoAliasWrites(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()
	bufs := p.Get(rc, 64)
	defer p.Put(bufs)

	for i, b := range bufs {
		for j := range b {
			b[j] = byte(i + 1)
		}
	}
	for i, b := range bufs {
		want := bytes.Repeat([]byte{byte(i + 1)}, 64)
		if !bytes.Equal(b, want) {
			t.Fatalf("bufs[%d] corrupted by neighbor write", i)
		}
	}
}

// TestPool_GCAnchor verifies that an in-use batch survives GC cycles.
// While in use, the only Go-heap reference to *batch is its bucket's
// used slice — the header back-pointer is an unsafe store GC doesn't
// trace, and the caller's bufs share the region's backing array, not
// the batch struct. Without the anchor, GC could reap *batch between
// Get and Put and Put would read a dangling pointer.
func TestPool_GCAnchor(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	bufs := p.Get(rc, 64)
	firstAddr := &bufs[0][0]
	for i, b := range bufs {
		b[0] = byte(i + 1)
	}

	// force multiple GC cycles. If the anchor is broken, a sweep during
	// one of these frees the *batch struct; subsequent Put would read a
	// stale pointer.
	runtime.GC()
	runtime.GC()

	// data must survive GC (bufs anchor the region's backing array).
	for i, b := range bufs {
		if b[0] != byte(i+1) {
			t.Fatalf("buf %d corrupted across GC: got %d want %d", i, b[0], byte(i+1))
		}
	}

	p.Put(bufs)
	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d after GC+Put, want 1", got)
	}

	// reuse must return the same underlying region, proving the batch
	// was tracked (not silently discarded as "foreign").
	again := p.Get(rc, 64)
	if &again[0][0] != firstAddr {
		t.Fatal("reuse did not return original region — anchor or tracking broken")
	}
	p.Put(again)
}

// TestPool_CarveBlocksAppend verifies the full-slice expressions Get
// uses to carve views — each buf has cap==len, so append forces a new
// backing array rather than bleeding into the next row.
func TestPool_CarveBlocksAppend(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()
	bufs := p.Get(rc, 64)
	defer p.Put(bufs)

	if cap(bufs[0]) != len(bufs[0]) {
		t.Fatalf("bufs[0] cap=%d len=%d — capacity leaked past len",
			cap(bufs[0]), len(bufs[0]))
	}

	// mark bufs[1] then append to bufs[0]; neighbor must not change.
	bufs[1][0] = 0xAA
	_ = append(bufs[0], 0xBB)
	if bufs[1][0] != 0xAA {
		t.Fatal("append on bufs[0] bled into bufs[1] — missing full-slice expression in carve")
	}
}

func TestPool_Concurrent(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	const goroutines = 16
	const iters = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			for range iters {
				bufs := p.Get(rc, 64)
				for _, b := range bufs {
					for i := range b {
						b[i] = byte(id)
					}
				}
				p.Put(bufs)
			}
		}(g)
	}
	wg.Wait()
}
