package fibre

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

const shardSize = int64(128) // arbitrary per-reserve size in bytes

func TestOccupancyReserveWithinBudget(t *testing.T) {
	o := newOccupancy(10 * shardSize)
	require.True(t, o.reserve(shardSize))
	require.True(t, o.reserve(shardSize))
	require.Equal(t, 2*shardSize, o.usage())
}

func TestOccupancyReserveAtLimit(t *testing.T) {
	o := newOccupancy(2 * shardSize)
	require.True(t, o.reserve(shardSize))
	require.True(t, o.reserve(shardSize), "exactly at budget still fits")
	require.False(t, o.reserve(shardSize), "over budget must be rejected")
	require.Equal(t, 2*shardSize, o.usage(), "a rejected reserve must not count")
}

func TestOccupancyNoLimit(t *testing.T) {
	o := newOccupancy(0) // disabled
	require.True(t, o.reserve(1<<40))
	require.True(t, o.reserve(1<<40))
	require.Equal(t, int64(2)<<40, o.usage(), "usage is still tracked with no limit")
}

func TestOccupancySeedAndRelease(t *testing.T) {
	o := newOccupancy(10 * shardSize)
	o.seed(3 * shardSize)
	require.Equal(t, 3*shardSize, o.usage())

	require.True(t, o.reserve(shardSize))
	o.release(2 * shardSize)
	require.Equal(t, 2*shardSize, o.usage(), "3 seeded + 1 reserved - 2 released")
}

// TestOccupancyConcurrentReserve hammers reserve from many goroutines at the
// budget boundary and checks the counter never over-admits and never loses a
// reservation. Run with -race.
func TestOccupancyConcurrentReserve(t *testing.T) {
	const capacity = 100
	budget := capacity * shardSize
	o := newOccupancy(budget)

	const goroutines = 4 * capacity // oversubscribe so most contend at the edge
	var (
		wg        sync.WaitGroup
		start     = make(chan struct{})
		succeeded atomic.Int64
	)
	for range goroutines {
		wg.Go(func() {
			<-start // release all at once to maximize contention
			if o.reserve(shardSize) {
				succeeded.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()

	admitted := succeeded.Load()
	require.LessOrEqual(t, o.usage(), budget, "must never admit past the budget")
	require.Equal(t, admitted*shardSize, o.usage(), "usage must equal exactly the admitted reservations")
	require.LessOrEqual(t, admitted, int64(capacity), "cannot admit more than capacity")
	require.Positive(t, admitted, "some reservations should succeed")
}
