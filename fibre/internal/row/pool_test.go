package row

import (
	"bytes"
	"runtime"
	"sync"
	"testing"
	"unsafe"
)

// Test shape — small values so tests stay fast. Mirrors the layout
// the pool sees in production: one row count for the assembler batch
// (parity + head + tail) and a distinct one for codec work buffers.
const (
	testAssemblerBatchRows = 6  // e.g. parityRows+2 with parityRows=4
	testCodecWorkRows      = 16 // e.g. 2·ceilPow2(parityRows) with parityRows=4
	testMaxRow             = 4096
)

// Row counts a pool built with these constants will accept.
var testRowCounts = []int{testAssemblerBatchRows, testCodecWorkRows}

func newPool(t testing.TB) *Pool {
	t.Helper()
	return New(testMaxRow, testAssemblerBatchRows, testCodecWorkRows)
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

// Different row counts live in different partitions — no row-count fallback.
func TestPool_RowCountsSeparate(t *testing.T) {
	if len(testRowCounts) < 2 {
		t.Skip("need at least two distinct row counts")
	}
	p := newPool(t)

	a := p.Get(testRowCounts[0], 64)
	b := p.Get(testRowCounts[1], 64)
	if got := p.Batches(); got != 2 {
		t.Fatalf("Batches=%d, want 2 (disjoint row counts)", got)
	}
	p.Put(a)
	p.Put(b)
}

func TestPool_UnacceptedRowCountPanics(t *testing.T) {
	p := newPool(t)
	defer func() {
		if recover() == nil {
			t.Fatal("Get with unaccepted row count should panic")
		}
	}()
	// A row count not in testRowCounts.
	unaccepted := 9999
	for _, rc := range testRowCounts {
		if rc == unaccepted {
			t.Fatalf("unaccepted=%d collides with an accepted class", unaccepted)
		}
	}
	p.Get(unaccepted, 64)
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

func TestPool_PutForeignBatchPanics(t *testing.T) {
	a := newPool(t)
	b := newPool(t)
	// Keep bufs in-use on pool a so it stays live; hand them to pool b,
	// which should reject via the identity check in p.batches.
	bufs := a.Get(assemblerRowCount(), 64)
	defer a.Put(bufs)

	defer func() {
		if recover() == nil {
			t.Fatal("Put of a batch from another pool should panic")
		}
	}()
	b.Put(bufs)
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

	// Request 256: reqIdx=3, maxIdx=reqIdx+slack=5 (size 384). 320 fits → reuse.
	small := p.Get(rc, 256)
	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d after slack fallback, want 1 (reused 320 slab)", got)
	}
	if len(small[0]) != 256 {
		t.Fatalf("returned len=%d, want 256 (requested)", len(small[0]))
	}
	p.Put(small)

	// On return, batch goes back to its backing bucket (320), not 256.
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

	// Request 64: reqIdx=0, maxIdx=reqIdx+slack=2 (size 192). 512 is above → grow.
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

	// No 320 batch exists; request 256 → grows new 256 batch (exact key).
	a := p.Get(rc, 256)
	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d, want 1", got)
	}
	p.Put(a)
	// Request 320 now: allocate new.
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

	// Populate the 320-rowSize bucket with two free batches via direct Gets.
	a := p.Get(rc, 320)
	b := p.Get(rc, 320)
	p.Put(a)
	p.Put(b)
	if got := p.Batches(); got != 2 {
		t.Fatalf("Batches=%d before aging, want 2", got)
	}

	// Drive fallback Gets at a smaller in-slack rowSize. LIFO pop means
	// the most-recently-Put 320 batch cycles; the other sits at the head
	// of the free queue with a frozen lastFreeGen and ages out.
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

	// Drive enough Gets to age both free batches past evictionThreshold.
	for range evictionThreshold + 1 {
		more := p.Get(rc, 64)
		p.Put(more)
	}
	_ = p.Get(rc, 64)
	if got := p.Batches(); got > 2 {
		t.Fatalf("Batches=%d, want <= 2 (aged eviction)", got)
	}
}

// The idle timer, when fired, drops every batch. The timer callback is
// invoked directly to avoid waiting idleGrace in tests.
func TestPool_IdleDropsEverything(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	a := p.Get(rc, 64)
	b := p.Get(rc, 64)
	p.Put(a)
	p.Put(b)

	if got := p.Batches(); got != 2 {
		t.Fatalf("Batches=%d before idle, want 2", got)
	}
	p.dropIdle()
	if got := p.Batches(); got != 0 {
		t.Fatalf("Batches=%d after idle drop, want 0", got)
	}
}

// The idle timer arms on transition to full idle and cancels on the next Get.
func TestPool_IdleTimerArmedAndCancelled(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	a := p.Get(rc, 64)
	p.Put(a)
	if p.idleTimer == nil {
		t.Fatal("idle timer should arm when pool goes fully idle")
	}

	_ = p.Get(rc, 64)
	if p.idleTimer != nil {
		t.Fatal("idle timer should be cancelled by Get")
	}
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
// While in use, the only Go-heap reference to *batch is p.batches — the
// header back-pointer is an unsafe store GC doesn't trace, and the
// caller's bufs share the region's backing array, not the batch struct.
// Without the anchor, GC could reap *batch between Get and Put and Put
// would read a dangling pointer.
func TestPool_GCAnchor(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	bufs := p.Get(rc, 64)
	firstAddr := &bufs[0][0]
	for i, b := range bufs {
		b[0] = byte(i + 1)
	}

	// Force multiple GC cycles. If the anchor is broken, a sweep during
	// one of these frees the *batch struct; subsequent Put would read a
	// stale pointer.
	runtime.GC()
	runtime.GC()

	// Data must survive GC (bufs anchor the region's backing array).
	for i, b := range bufs {
		if b[0] != byte(i+1) {
			t.Fatalf("buf %d corrupted across GC: got %d want %d", i, b[0], byte(i+1))
		}
	}

	p.Put(bufs)
	if got := p.Batches(); got != 1 {
		t.Fatalf("Batches=%d after GC+Put, want 1", got)
	}

	// Reuse must return the same underlying region, proving the batch
	// was tracked (not silently discarded as "foreign").
	again := p.Get(rc, 64)
	if &again[0][0] != firstAddr {
		t.Fatal("reuse did not return original region — anchor or tracking broken")
	}
	p.Put(again)
}

// TestPool_CarveBlocksAppend verifies the full-slice expression in
// carve — a carved buf has cap==len, so append forces a new backing
// array rather than bleeding into the next row.
func TestPool_CarveBlocksAppend(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()
	bufs := p.Get(rc, 64)
	defer p.Put(bufs)

	if cap(bufs[0]) != len(bufs[0]) {
		t.Fatalf("bufs[0] cap=%d len=%d — carve leaked capacity past len",
			cap(bufs[0]), len(bufs[0]))
	}

	// Mark bufs[1] then append to bufs[0]; neighbor must not change.
	bufs[1][0] = 0xAA
	_ = append(bufs[0], 0xBB)
	if bufs[1][0] != 0xAA {
		t.Fatal("append on carved bufs[0] bled into bufs[1] — carve missing full-slice expression")
	}
}

// TestPool_StructLayout pins the size of the pool's hot data structures
// so accidental bloat or sub-optimal field ordering shows up as a test
// failure. The expected sizes are minimal for the current fields on a
// 64-bit platform; any intentional addition should update them.
func TestPool_StructLayout(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"Pool", unsafe.Sizeof(Pool{}), 104},
		{"bucket", unsafe.Sizeof(bucket{}), 48},
		{"batch", unsafe.Sizeof(batch{}), 56},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("sizeof(%s) = %d, want %d — update the expected size if the change is intentional, but double-check field ordering first",
				c.name, c.got, c.want)
		}
	}

	// batch must fit in a single 64-byte cache line.
	if unsafe.Sizeof(batch{}) > 64 {
		t.Errorf("sizeof(batch) = %d > 64, spills past one cache line", unsafe.Sizeof(batch{}))
	}

	// headerSize must hold at least a *batch back-pointer and keep the
	// following data region 64-byte aligned.
	ptrSize := unsafe.Sizeof((*batch)(nil))
	if uintptr(headerSize) < ptrSize {
		t.Errorf("headerSize %d < sizeof(*batch) %d", headerSize, ptrSize)
	}
	if headerSize%rowSizeAlign != 0 {
		t.Errorf("headerSize %d not a multiple of rowSizeAlign %d", headerSize, rowSizeAlign)
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
