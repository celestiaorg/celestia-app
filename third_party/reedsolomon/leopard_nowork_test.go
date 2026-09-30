package reedsolomon

import (
	"bytes"
	"math/rand"
	"testing"
)

type countingAllocator struct{ gets int }

func (a *countingAllocator) Get(n, size int) [][]byte {
	a.gets++
	return AllocAligned(n, size)
}

func (a *countingAllocator) Put([][]byte) {}

// TestLeopardFusedTopSkipsWork checks that encodes on the fused top-stage
// path request no work buffers and still match the portable encoder.
func TestLeopardFusedTopSkipsWork(t *testing.T) {
	for _, sh := range []struct{ data, parity, size int }{{4096, 12288, 128}, {16, 48, 2112}, {64, 200, 192}, {4, 9, 2112}, {4, 12, 64}, {1, 3, 64}, {32, 96, 128}} {
		ref, err := New(sh.data, sh.parity, WithLeopardGF16(true), WithNEON(false))
		if err != nil {
			t.Fatal(err)
		}
		alloc := &countingAllocator{}
		enc, err := New(sh.data, sh.parity, WithLeopardGF16(true), WithWorkAllocator(alloc))
		if err != nil {
			t.Fatal(err)
		}
		rng := rand.New(rand.NewSource(int64(sh.data)))
		want := AllocAligned(sh.data+sh.parity, sh.size)
		got := AllocAligned(sh.data+sh.parity, sh.size)
		for i := range want {
			rng.Read(want[i])
			copy(got[i], want[i])
		}
		if err := ref.Encode(want); err != nil {
			t.Fatal(err)
		}
		if err := enc.Encode(got); err != nil {
			t.Fatal(err)
		}
		for i := range want {
			if !bytes.Equal(want[i], got[i]) {
				t.Fatalf("%d+%d: shard %d differs", sh.data, sh.parity, i)
			}
		}
		m := ceilPow2(sh.parity)
		fused := defaultOptions.useNEON && fuseTopStages(m, sh.data) && sh.parity%(m/4) == 0
		if fused && alloc.gets != 0 || !fused && alloc.gets != 1 {
			t.Fatalf("%d+%d: fused=%v, %d work requests", sh.data, sh.parity, fused, alloc.gets)
		}
	}
}
