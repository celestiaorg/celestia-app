package fibre

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

func TestUploadCoordinator(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var c uploadCoordinator
		hash := make([]byte, 32)
		release, err := c.acquire(t.Context(), hash)
		require.NoError(t, err)

		acquired := make(chan func(), 1)
		go func() {
			next, err := c.acquire(t.Context(), hash)
			if err != nil {
				t.Error(err)
				return
			}
			acquired <- next
		}()
		synctest.Wait()
		require.Empty(t, acquired, "duplicate must wait for its owner")
		require.Len(t, c.owners, 1)
		release()
		next := <-acquired
		require.Len(t, c.owners, 1)
		next()
		require.Empty(t, c.owners)
	})
}

func TestUploadCoordinatorCancelledWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var c uploadCoordinator
		hash := make([]byte, 32)
		release, err := c.acquire(t.Context(), hash)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := c.acquire(ctx, hash); done <- err }()
		synctest.Wait()
		require.Empty(t, done)
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		require.Len(t, c.owners, 1, "cancelled waiter must not remove the owner")
		release()
		require.Empty(t, c.owners)
		_, err = c.acquire(ctx, hash)
		require.ErrorIs(t, err, context.Canceled)
		require.Empty(t, c.owners)
	})
}
