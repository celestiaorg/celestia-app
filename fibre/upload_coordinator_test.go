package fibre

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUploadCoordinator(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var c uploadCoordinator
		hash := make([]byte, 32)
		release, err := c.acquire(t.Context(), hash)
		require.NoError(t, err)
		otherHash := append([]byte(nil), hash...)
		otherHash[31] = 1
		other, err := c.acquire(t.Context(), otherHash)
		require.NoError(t, err)
		other()

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
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var c uploadCoordinator
				hash := make([]byte, 32)
				release, err := c.acquire(t.Context(), hash)
				require.NoError(t, err)
				ctx, cancel := context.WithCancel(t.Context())
				want := context.Canceled
				if deadline {
					cancel()
					ctx, cancel = context.WithTimeout(t.Context(), time.Second)
					want = context.DeadlineExceeded
				}
				defer cancel()
				done := make(chan error, 1)
				go func() { _, err := c.acquire(ctx, hash); done <- err }()
				synctest.Wait()
				require.Empty(t, done)
				if deadline {
					time.Sleep(time.Second)
				} else {
					cancel()
				}
				require.ErrorIs(t, <-done, want)
				require.Len(t, c.owners, 1, "cancelled waiter must not remove the owner")
				release()
				require.Empty(t, c.owners)
				_, err = c.acquire(ctx, hash)
				require.ErrorIs(t, err, want)
				require.Empty(t, c.owners)
			})
		})
	}
}
