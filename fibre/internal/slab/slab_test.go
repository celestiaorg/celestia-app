package slab

import (
	"bytes"
	"sync"
	"testing"
)

func TestPool_GetPut(t *testing.T) {
	p := New()

	bufs := p.Get(4, 32)
	if len(bufs) != 4 {
		t.Fatalf("Get returned %d bufs, want 4", len(bufs))
	}
	for i, b := range bufs {
		if len(b) != 32 {
			t.Fatalf("bufs[%d] len=%d, want 32", i, len(b))
		}
	}

	// writes to separate buffers must not alias
	for i, b := range bufs {
		for j := range b {
			b[j] = byte(i + 1)
		}
	}
	for i, b := range bufs {
		if !bytes.Equal(b, bytes.Repeat([]byte{byte(i + 1)}, 32)) {
			t.Fatalf("bufs[%d] corrupted by neighbor write", i)
		}
	}

	p.Put(bufs)
}

func TestPool_GrowsAndShrinks(t *testing.T) {
	p := New()

	a := p.Get(4, 64) // creates first slab (256 bytes)
	b := p.Get(4, 64) // first slab full, grows second
	if p.Slabs() != 2 {
		t.Fatalf("Slabs()=%d after growth, want 2", p.Slabs())
	}

	p.Put(a)
	p.Put(b)
	// put does not shrink — slabs retained for reuse.
	if p.Slabs() != 2 {
		t.Fatalf("Slabs()=%d after Put, want 2 (no shrink)", p.Slabs())
	}
	// explicit Shrink releases fully-free slabs once the pool goes idle.
	p.Shrink()
	if p.Slabs() != 0 {
		t.Fatalf("Slabs()=%d after idle Shrink, want 0", p.Slabs())
	}
}

func TestPool_GrowUsesOnlyDeficit(t *testing.T) {
	p := New()

	a := p.Get(2, 64) // slab1 = 128
	b := p.Get(4, 64) // slab2 should match the unsatisfied deficit: 256
	if got := p.Slabs(); got != 2 {
		t.Fatalf("Slabs()=%d, want 2", got)
	}
	if got := p.slabs[1].size; got != 256 {
		t.Fatalf("slab2 size=%d, want 256", got)
	}

	p.Put(a)
	p.Put(b)
}

func TestPool_ScatteredGet(t *testing.T) {
	p := New()

	// create a slab with a hole in the middle
	a := p.Get(1, 64) // [0..64)
	b := p.Get(1, 64) // [64..128)
	c := p.Get(1, 64) // [128..192)

	// free the middle one, creating a hole
	p.Put(b)

	// get 2 buffers — should gather from the hole + grow or use remaining space
	d := p.Get(2, 64)
	if len(d) != 2 {
		t.Fatalf("scattered Get returned %d, want 2", len(d))
	}

	// verify no aliasing
	for j := range d[0] {
		d[0][j] = 0xAA
	}
	for j := range d[1] {
		d[1][j] = 0xBB
	}
	if d[0][0] == d[1][0] {
		t.Fatal("scattered buffers alias")
	}

	p.Put(a)
	p.Put(c)
	p.Put(d)
}

func TestPool_SmallBlobsPack(t *testing.T) {
	p := New()

	// first Get creates a slab of 640 bytes; Put doesn't shrink so it stays.
	first := p.Get(10, 64)
	p.Put(first)

	// 10 individual Gets should reuse the retained slab.
	handles := make([][]byte, 0, 10)
	for range 10 {
		bufs := p.Get(1, 64)
		handles = append(handles, bufs...)
	}
	if p.Slabs() != 1 {
		t.Fatalf("Slabs()=%d, want 1 (all should fit)", p.Slabs())
	}
	for _, b := range handles {
		p.Put([][]byte{b})
	}
}

func TestPool_MixedSizes(t *testing.T) {
	p := New()

	large := p.Get(1, 512)
	small1 := p.Get(1, 128)
	small2 := p.Get(1, 128)

	// free the large region, allocate something else in the gap
	p.Put(large)
	medium := p.Get(1, 256) // fits in the freed 512-byte gap
	if len(medium) != 1 {
		t.Fatal("medium alloc failed")
	}

	p.Put(small1)
	p.Put(small2)
	p.Put(medium)
}

func TestPool_PartialFree(t *testing.T) {
	p := New()

	bufs := p.Get(8, 64) // 512 bytes in one slab

	// free odd-indexed buffers, leaving 4 scattered holes.
	p.Put([][]byte{bufs[1], bufs[3], bufs[5], bufs[7]})

	// a smaller follow-up Get should weave into the freed holes without
	// needing a second slab.
	more := p.Get(4, 64)
	if len(more) != 4 {
		t.Fatalf("got %d, want 4", len(more))
	}
	if p.Slabs() != 1 {
		t.Fatalf("Slabs()=%d, want 1 (freed slots should be reused)", p.Slabs())
	}

	p.Put([][]byte{bufs[0], bufs[2], bufs[4], bufs[6]})
	p.Put(more)
}

func TestPool_ShrinkGraceWhileActive(t *testing.T) {
	p := New()

	a := p.Get(2, 64) // slab1 = 128, stays live
	b := p.Get(4, 64) // slab2 = 256, becomes the free candidate
	if p.Slabs() != 2 {
		t.Fatalf("Slabs()=%d, want 2", p.Slabs())
	}
	p.Put(b)

	// first terminal boundary while another slab is still live: the grace
	// period keeps the free slab warm rather than evicting it immediately.
	p.Shrink()
	if p.Slabs() != 2 {
		t.Fatalf("Slabs()=%d after first active Shrink, want 2", p.Slabs())
	}
	if got := p.slabs[1].idleShrinks; got != 1 {
		t.Fatalf("idleShrinks=%d after first active Shrink, want 1", got)
	}

	// second terminal boundary with the same active/free split ages the free
	// slab past the grace period, so it becomes evictable.
	p.Shrink()
	if p.Slabs() != 1 {
		t.Fatalf("Slabs()=%d after second active Shrink, want 1", p.Slabs())
	}

	p.Put(a)
}

func TestPool_Concurrent(t *testing.T) {
	p := New()

	const goroutines = 16
	const iters = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			for range iters {
				bufs := p.Get(4, 32)
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
