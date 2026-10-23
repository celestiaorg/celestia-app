package app

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForEachParallel(t *testing.T) {
	for _, n := range []int{0, 1, 3, 1000} {
		counts := make([]atomic.Int32, n)
		forEachParallel(n, func(i int) { counts[i].Add(1) })()
		for i := range counts {
			require.EqualValues(t, 1, counts[i].Load(), "n=%d index %d", n, i)
		}
	}

	// A panic in a worker reaches the caller only through wait, so
	// ProcessProposal's own recover can turn it into a rejection.
	wait := forEachParallel(100, func(i int) {
		if i == 42 {
			panic("boom")
		}
	})
	require.PanicsWithValue(t, "boom", wait)
}
