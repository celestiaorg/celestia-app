package row

import "testing"

func TestPoolStatsLifecycle(t *testing.T) {
	p := NewPool(4096, 512)
	first := p.Get(512, 4096)
	s := p.Stats()
	if s.InUseSlabs != 1 || s.FreeSlabs != 0 || s.InUseBytes < 512*4096 || s.Requests != 1 || s.Allocations != 1 {
		t.Fatalf("allocated stats: %+v", s)
	}
	if !disableMmap && s.MappedBytes != s.InUseBytes {
		t.Fatalf("mapped slab missing: %+v", s)
	}
	capacity := s.InUseBytes
	p.Put(first)
	s = p.Stats()
	if s.InUseBytes != 0 || s.FreeBytes != capacity || s.FreeSlabs != 1 {
		t.Fatalf("returned stats: %+v", s)
	}
	// A smaller request reuses the larger slab; report capacity and request separately.
	second := p.Get(512, 4032)
	s = p.Stats()
	if s.InUseBytes != capacity || s.FreeBytes != 0 || s.Requests != 2 || s.Allocations != 1 || s.LastRequestedRowSize != 4032 || s.MaxRequestedRowSize != 4096 {
		t.Fatalf("reused stats: %+v", s)
	}
	p.Put(second)
	bk := bucketAt(p, 4096)
	bk.Lock()
	bk.cancelIdle()
	bk.Unlock()
	bk.dropIdle()
	s = p.Stats()
	if s.InUseSlabs != 0 || s.FreeSlabs != 0 || s.MappedBytes != 0 || s.FreeBytes != 0 || s.Requests != 2 {
		t.Fatalf("evicted stats: %+v", s)
	}
}
