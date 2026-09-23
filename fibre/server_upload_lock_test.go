package fibre

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUploadCoordinatorIndependentHashes(t *testing.T) {
	var c uploadCoordinator
	release, err := c.acquire(t.Context(), []byte{1, 2, 3})
	require.NoError(t, err)
	defer release()
	// These hashes shared a stripe when only the first two bytes selected a mutex.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	other, err := c.acquire(ctx, []byte{1, 2, 4})
	require.NoError(t, err)
	other()
	canceled, stop := context.WithCancel(t.Context())
	stop()
	_, err = c.acquire(canceled, []byte{1, 2, 3})
	require.ErrorIs(t, err, context.Canceled)
}

func TestUploadCoordinatorExclusionAndCleanup(t *testing.T) {
	var c uploadCoordinator
	var active atomic.Int64
	var overlap atomic.Bool
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			release, err := c.acquire(t.Context(), []byte("same"))
			if err != nil {
				t.Error(err)
				return
			}
			if active.Add(1) != 1 {
				overlap.Store(true)
			}
			time.Sleep(time.Microsecond)
			active.Add(-1)
			release()
		})
	}
	wg.Wait()
	require.False(t, overlap.Load())
	require.Empty(t, c.owners)
}

func TestUploadCoordinatorCanceledWaiter(t *testing.T) {
	var c uploadCoordinator
	release, err := c.acquire(t.Context(), []byte("same"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err = c.acquire(ctx, []byte("same"))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Len(t, c.owners, 1)
	release()
	require.Empty(t, c.owners)
	next, err := c.acquire(t.Context(), []byte("same"))
	require.NoError(t, err)
	next()
}
