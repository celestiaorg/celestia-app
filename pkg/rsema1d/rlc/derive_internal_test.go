package rlc

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

// TestDeriveRangeSplittable verifies deriveRange's output over [0, n) is
// identical to the concatenation of its output over any partition of [0, n)
// into contiguous sub-ranges. This is the invariant Derive's parallel path
// depends on: workers compute disjoint index spans and the result must be
// equivalent to a single-threaded fill.
func TestDeriveRangeSplittable(t *testing.T) {
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i * 7)
	}
	const n = 1024

	whole := make(Vector, n)
	deriveRange(seed, whole, 0, n)

	partitions := [][]int{
		{0, n / 2, n},                   // 2 even pieces
		{0, n / 4, n / 2, 3 * n / 4, n}, // 4 even pieces
		{0, 1, 100, 500, 999, n},        // uneven pieces, including length-1
	}
	for _, p := range partitions {
		got := make(Vector, n)
		for i := 0; i < len(p)-1; i++ {
			deriveRange(seed, got, p[i], p[i+1])
		}
		for i := range whole {
			if !field.Equal128(whole[i], got[i]) {
				t.Fatalf("partition=%v differs at i=%d", p, i)
			}
		}
	}
}
