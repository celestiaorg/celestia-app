package row

import (
	"bytes"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Test shape — small values so tests stay fast.
const (
	testAssemblerBatchRows = 6  // e.g. parityRows+2 with parityRows=4
	testCodecWorkRows      = 16 // e.g. 2·ceilPow2(parityRows) with parityRows=4
	testMaxRow             = 4096
)

// newPool returns a Pool with its own free list, so tests can count slabs
// without seeing other tests' memory.
func newPool(t testing.TB) *Pool {
	t.Helper()
	p := NewPool(testMaxRow, testAssemblerBatchRows)
	p.a = newAllocator()
	return p
}

// Slabs counts in-use plus free slabs.
func (p *Pool) Slabs() int {
	p.a.mu.Lock()
	defer p.a.mu.Unlock()
	return len(p.a.inUse) + len(p.a.free)
}

// Primary row count used by most tests (assembler slab).
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
	if got := p.Slabs(); got != 1 {
		t.Fatalf("Slabs=%d after Put, want 1", got)
	}

	// second Get at exact key reuses the same slab.
	more := p.Get(rc, 64)
	if got := p.Slabs(); got != 1 {
		t.Fatalf("Slabs=%d after exact-match reuse, want 1", got)
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
			t.Fatal("second Put of same slab should panic")
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

func TestPool_GetZeroPanics(t *testing.T) {
	p := newPool(t)
	defer func() {
		if recover() == nil {
			t.Fatal("Get(0, _) should panic")
		}
	}()
	p.Get(0, 64)
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

// TestPool_GCAnchor verifies that an in-use Go-heap slab survives GC cycles
// and is still recognized by Put: the allocator's in-use map, not the
// caller's row views, is what keeps the whole slab tracked.
func TestPool_GCAnchor(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	bufs := p.Get(rc, 64)
	firstAddr := &bufs[0][0]
	for i, b := range bufs {
		b[0] = byte(i + 1)
	}

	// force multiple GC cycles. If the anchor is broken, a sweep during
	// one of these frees the *slab struct; subsequent Put would read a
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
	if got := p.Slabs(); got != 1 {
		t.Fatalf("Slabs=%d after GC+Put, want 1", got)
	}

	// reuse must return the same underlying region, proving the slab
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

// A free slab up to maxWaste times the request is reused; the caller sees
// exactly the size it asked for.
func TestPool_ReusesLargerFreeSlab(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	big := p.Get(rc, 128)
	p.Put(big)
	small := p.Get(rc, 64)
	if got := p.Slabs(); got != 1 {
		t.Fatalf("Slabs=%d, want 1 (128-byte rows reused for 64)", got)
	}
	if len(small[0]) != 64 || &small[0][0] != &big[0][0] {
		t.Fatal("expected the freed slab, carved at the requested size")
	}
	p.Put(small)
}

// A free slab more than maxWaste times the request is not reused.
func TestPool_LargeWasteAllocates(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	p.Put(p.Get(rc, 192))
	small := p.Get(rc, 64)
	if got := p.Slabs(); got != 2 {
		t.Fatalf("Slabs=%d, want 2 (192 > 2x64 must not be reused)", got)
	}
	p.Put(small)
}

// Get takes the smallest fitting slab, the most recently freed among equals.
func TestPool_BestFit(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	a, b, c, d := p.Get(rc, 256), p.Get(rc, 192), p.Get(rc, 192), p.Get(rc, 128)
	p.Put(a)
	p.Put(b)
	p.Put(c)
	p.Put(d)

	got := p.Get(rc, 192) // d too small, a fits but is larger, c freed after b
	if &got[0][0] != &c[0][0] {
		t.Fatal("expected the most recently freed 192-byte slab")
	}
	p.Put(got)
}

// Slabs from any Pool share one free list.
func TestPool_SharedAcrossPools(t *testing.T) {
	a := newPool(t)
	b := NewPool(testMaxRow, 2*testAssemblerBatchRows)
	b.a = a.a

	rows := a.Get(testAssemblerBatchRows, 128)
	a.Put(rows)
	got := b.Get(2*testAssemblerBatchRows, 64) // same bytes, other shape
	if &got[0][0] != &rows[0][0] {
		t.Fatal("expected b to reuse the slab a freed")
	}
	b.Put(got)
}

// Free slabs beyond maxFreeBytes are released, oldest first.
func TestPool_MaxFreeBytes(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()
	p.a.maxFree = 2 * rc * 64

	x, y, z := p.Get(rc, 64), p.Get(rc, 64), p.Get(rc, 64)
	p.Put(x)
	p.Put(y)
	p.Put(z)
	if got := p.Slabs(); got != 2 {
		t.Fatalf("Slabs=%d, want 2", got)
	}
	if got := p.Get(rc, 64); &got[0][0] != &z[0][0] {
		t.Fatal("expected the newest slab to survive")
	}
}

// Free slabs idle for idleTTL are released.
func TestPool_IdleTTL(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	p.Put(p.Get(rc, 64))
	keep := p.Get(rc, 128)
	p.a.mu.Lock()
	expired := p.a.trimLocked(time.Now().Add(idleTTL))
	p.a.mu.Unlock()
	if len(expired) != 1 || p.Slabs() != 1 {
		t.Fatalf("expired=%d Slabs=%d, want 1 and 1 (only the in-use slab)", len(expired), p.Slabs())
	}
	p.Put(keep)
}

// The background trim releases idle slabs without further Puts.
func TestPool_TrimLoop(t *testing.T) {
	p := newPool(t)
	p.a.ttl = 20 * time.Millisecond

	p.Put(p.Get(assemblerRowCount(), 64))
	deadline := time.Now().Add(5 * time.Second)
	for p.Slabs() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("idle slab was not released")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Put rejects a buffer that is not the start of an in-use slab.
func TestPool_PutForeignPanics(t *testing.T) {
	p := newPool(t)
	rows := p.Get(assemblerRowCount(), 64)
	defer p.Put(rows)
	defer func() {
		if recover() == nil {
			t.Fatal("Put of a row other than the first should panic")
		}
	}()
	p.PutRegion(rows[1])
}

// GetRegion and PutRegion round-trip through the same free list as Get/Put.
func TestPool_Region(t *testing.T) {
	p := newPool(t)
	rc := assemblerRowCount()

	region := p.GetRegion(rc, 64)
	if len(region) != rc*64 || cap(region) != rc*64 {
		t.Fatalf("len=%d cap=%d, want %d", len(region), cap(region), rc*64)
	}
	p.PutRegion(region[:0])
	rows := p.Get(rc, 64)
	if &rows[0][0] != &region[0] {
		t.Fatal("expected the region's slab to be reused")
	}
	p.Put(rows)
}
