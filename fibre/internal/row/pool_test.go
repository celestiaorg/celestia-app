package row

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

// Test shape — small values so tests stay fast.
const (
	testRows   = 6 // e.g. parityRows+2 with parityRows=4
	testMaxRow = 4096
)

// slabCount counts in-use plus free slabs.
func (a *allocator) slabCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.inUse) + len(a.free)
}

// newTestAllocator returns a private allocator whose trim loop stops with the
// test.
func newTestAllocator(t *testing.T) *allocator {
	a := newAllocator()
	t.Cleanup(a.stop)
	return a
}

func expectPanic(t *testing.T, msg string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal(msg)
		}
	}()
	f()
}

// Get carves rows that do not overlap and whose capacity stops at their
// length, so neither a write nor an append reaches a neighbor.
func TestPool_GetCarvesRows(t *testing.T) {
	p := NewPool(testMaxRow, testRows)
	bufs := p.Get(testRows, 64)
	defer p.Put(bufs)

	if len(bufs) != testRows {
		t.Fatalf("Get returned %d bufs, want %d", len(bufs), testRows)
	}
	for i, b := range bufs {
		if len(b) != 64 || cap(b) != 64 {
			t.Fatalf("bufs[%d] len=%d cap=%d, want 64 and 64", i, len(b), cap(b))
		}
		for j := range b {
			b[j] = byte(i + 1)
		}
	}
	for i, b := range bufs {
		if !bytes.Equal(b, bytes.Repeat([]byte{byte(i + 1)}, 64)) {
			t.Fatalf("bufs[%d] corrupted by neighbor write", i)
		}
	}
}

func TestPool_InvalidSizePanics(t *testing.T) {
	p := NewPool(testMaxRow, testRows)
	expectPanic(t, "Get with non-multiple-of-64 size should panic", func() { p.Get(testRows, 63) })
}

// Get with rowCount != pool.rowCount panics — a Pool serves only one
// row count.
func TestPool_RowCountMismatchPanics(t *testing.T) {
	p := NewPool(testMaxRow, testRows)
	expectPanic(t, "Get with rowCount != pool.rowCount should panic", func() { p.Get(testRows+1, 64) })
}

func TestPool_OversizeRowPanics(t *testing.T) {
	p := NewPool(testMaxRow, testRows)
	expectPanic(t, "Get with rowSize > maxRowSize should panic", func() { p.Get(testRows, testMaxRow+64) })
}

func TestPool_DoubleFreePanics(t *testing.T) {
	p := NewPool(testMaxRow, testRows)
	bufs := p.Get(testRows, 64)
	p.Put(bufs)
	expectPanic(t, "second Put of same slab should panic", func() { p.Put(bufs) })
}

func TestPool_PutEmptyPanics(t *testing.T) {
	p := NewPool(testMaxRow, testRows)
	expectPanic(t, "Put with empty bufs should panic", func() { p.Put(nil) })
}

func TestPool_GetZeroPanics(t *testing.T) {
	p := NewPool(testMaxRow, testRows)
	expectPanic(t, "Get(0, _) should panic", func() { p.Get(0, 64) })
}

func TestAlloc_NonPositivePanics(t *testing.T) {
	expectPanic(t, "Alloc(0) should panic", func() { Alloc(0) })
}

func TestPool_Concurrent(t *testing.T) {
	p := NewPool(testMaxRow, testRows)

	const goroutines = 16
	const iters = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			for range iters {
				bufs := p.Get(testRows, 64)
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

// The free-list tests below use a private allocator, so they can count slabs
// without seeing other tests' memory.

// A freed slab is reused by the next get of the same size; the caller sees
// exactly the size it asked for, and put needs only the slab's start.
func TestAllocator_GetPutExact(t *testing.T) {
	a := newTestAllocator(t)

	buf := a.get(384)
	if len(buf) != 384 || cap(buf) != 384 {
		t.Fatalf("len=%d cap=%d, want 384", len(buf), cap(buf))
	}
	a.put(buf[:0])
	if got := a.slabCount(); got != 1 {
		t.Fatalf("slabCount=%d after put, want 1", got)
	}

	again := a.get(384)
	if &again[0] != &buf[0] || a.slabCount() != 1 {
		t.Fatal("expected the freed slab to be reused")
	}
	a.put(again)
}

// A free slab up to maxWaste times the request is reused; the caller sees
// exactly the size it asked for.
func TestAllocator_ReusesLargerFreeSlab(t *testing.T) {
	a := newTestAllocator(t)

	big := a.get(128)
	a.put(big)
	small := a.get(64)
	if got := a.slabCount(); got != 1 {
		t.Fatalf("slabCount=%d, want 1 (128 bytes reused for 64)", got)
	}
	if len(small) != 64 || &small[0] != &big[0] {
		t.Fatal("expected the freed slab, cut to the requested size")
	}
	a.put(small)
}

// A free slab more than maxWaste times the request is not reused.
func TestAllocator_LargeWasteAllocates(t *testing.T) {
	a := newTestAllocator(t)

	a.put(a.get(192))
	small := a.get(64)
	if got := a.slabCount(); got != 2 {
		t.Fatalf("slabCount=%d, want 2 (192 > 2x64 must not be reused)", got)
	}
	a.put(small)
}

// get takes the smallest fitting slab, the most recently freed among equals.
func TestAllocator_BestFit(t *testing.T) {
	a := newTestAllocator(t)

	w, x, y, z := a.get(256), a.get(192), a.get(192), a.get(128)
	a.put(w)
	a.put(x)
	a.put(y)
	a.put(z)

	got := a.get(192) // z too small, w fits but is larger, y freed after x
	if &got[0] != &y[0] {
		t.Fatal("expected the most recently freed 192-byte slab")
	}
	a.put(got)
}

// Free slabs beyond maxFreeBytes are released, oldest first.
func TestAllocator_MaxFreeBytes(t *testing.T) {
	a := newTestAllocator(t)
	a.maxFree = 2 * 64

	x, y, z := a.get(64), a.get(64), a.get(64)
	a.put(x)
	a.put(y)
	a.put(z)
	if got := a.slabCount(); got != 2 {
		t.Fatalf("slabCount=%d, want 2", got)
	}
	if got := a.get(64); &got[0] != &z[0] {
		t.Fatal("expected the newest slab to survive")
	}
}

// Free slabs idle for idleTTL are released.
func TestAllocator_IdleTTL(t *testing.T) {
	a := newTestAllocator(t)

	a.put(a.get(64))
	keep := a.get(128)
	a.mu.Lock()
	expired := a.trimLocked(time.Now().Add(idleTTL))
	a.mu.Unlock()
	if len(expired) != 1 || a.slabCount() != 1 {
		t.Fatalf("expired=%d slabCount=%d, want 1 and 1 (only the in-use slab)", len(expired), a.slabCount())
	}
	a.put(keep)
}

// The background trim releases idle slabs without further puts.
func TestAllocator_TrimLoop(t *testing.T) {
	a := newTestAllocator(t)
	a.ttl = 20 * time.Millisecond

	a.put(a.get(64))
	deadline := time.Now().Add(5 * time.Second)
	for a.slabCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("idle slab was not released")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// put rejects a buffer that is not the start of an in-use slab.
func TestAllocator_PutForeignPanics(t *testing.T) {
	a := newTestAllocator(t)
	buf := a.get(128)
	defer a.put(buf)
	expectPanic(t, "put of a buffer inside a slab should panic", func() { a.put(buf[64:]) })
}
