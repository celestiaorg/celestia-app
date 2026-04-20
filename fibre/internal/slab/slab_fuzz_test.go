package slab

import (
	"fmt"
	"testing"
)

// FuzzPool exercises random Get/Put/Shrink sequences against the pool.
// Each byte of the fuzz input drives one operation (high nibble = op code,
// low nibble = parameter). After each op and at end-of-sequence the fuzzer
// verifies structural invariants: regions are sorted, disjoint, in-bounds,
// and fully coalesced.
func FuzzPool(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x13, 0x21, 0x04, 0x30})
	f.Add([]byte{0x05, 0x05, 0x05, 0x11, 0x11, 0x20})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, ops []byte) {
		p := New()
		var outstanding [][][]byte

		for _, op := range ops {
			code := op >> 4
			arg := int(op & 0x0F)

			switch code & 0x03 {
			case 0: // Get: n in [1,8], size in {64, 128}
				n := (arg & 0x07) + 1
				size := 64 << (arg >> 3 & 0x01) // 64 or 128
				bufs := p.Get(n, size)
				if len(bufs) != n {
					t.Fatalf("Get(%d, %d) returned %d bufs", n, size, len(bufs))
				}
				// smoke-write so race detector would notice aliasing.
				for i, buf := range bufs {
					if len(buf) != size {
						t.Fatalf("buf[%d] len=%d, want %d", i, len(buf), size)
					}
					buf[0] = byte(i)
				}
				outstanding = append(outstanding, bufs)

			case 1: // Put: release the oldest outstanding Get
				if len(outstanding) > 0 {
					p.Put(outstanding[0])
					outstanding = outstanding[1:]
				}

			case 2: // Put subset: release first half of the most recent Get
				if len(outstanding) > 0 {
					last := outstanding[len(outstanding)-1]
					half := len(last) / 2
					if half > 0 {
						p.Put(last[:half])
						outstanding[len(outstanding)-1] = last[half:]
					}
				}

			case 3: // Shrink
				p.Shrink()
			}
			assertSlabInvariants(t, p)
		}

		// drain any remaining outstanding and re-check end-state invariants.
		for _, bufs := range outstanding {
			p.Put(bufs)
		}
		assertSlabInvariants(t, p)
		p.Shrink()
		if slabs := p.Slabs(); slabs != 0 {
			t.Fatalf("Slabs=%d after drain+Shrink, want 0", slabs)
		}
	})
}

// assertSlabInvariants checks that every slab's regions list is sorted,
// disjoint, within bounds, and fully coalesced (no adjacent regions that
// should have merged). Test-only; accesses internal fields directly.
func assertSlabInvariants(t *testing.T, p *Pool) {
	t.Helper()
	for si, s := range p.slabs {
		for i, r := range s.regions {
			if err := regionError(s, i, r); err != "" {
				t.Fatalf("slab[%d]: %s", si, err)
			}
		}
	}
}

func regionError(s *slab, i int, r region) string {
	if r.size <= 0 {
		return fmt.Sprintf("region[%d] non-positive size %d", i, r.size)
	}
	if r.off < 0 || r.off+r.size > s.size {
		return fmt.Sprintf("region[%d] out of bounds off=%d size=%d slab=%d", i, r.off, r.size, s.size)
	}
	if i > 0 {
		prev := s.regions[i-1]
		if r.off < prev.off+prev.size {
			return fmt.Sprintf("region[%d] overlaps region[%d]: %+v vs %+v", i-1, i, prev, r)
		}
		if r.off == prev.off+prev.size {
			return fmt.Sprintf("region[%d] adjacent to region[%d] (coalesce miss): %+v, %+v", i-1, i, prev, r)
		}
	}
	return ""
}
