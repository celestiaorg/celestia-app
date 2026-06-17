package docker_e2e

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBusyboxImage_MatchesTastoraRef guards the busybox reference we pre-pull
// against tastora's internal one. tastora pulls busybox:stable
// (github.com/celestiaorg/tastora/framework/docker/internal.BusyboxRef) during
// chain-node initialization and skips the pull when the image is already cached
// locally. Pre-pulling must use the exact same ref for that cache hit to work;
// the internal package can't be imported, so this test documents the coupling.
func TestBusyboxImage_MatchesTastoraRef(t *testing.T) {
	assert.Equal(t, "busybox:stable", busyboxImage().Ref())
}

func TestRetryPull_SucceedsOnFirstAttempt(t *testing.T) {
	calls := 0
	err := retryPull(context.Background(), 3, time.Millisecond, func() error {
		calls++
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestRetryPull_SucceedsAfterTransientFailures(t *testing.T) {
	calls := 0
	err := retryPull(context.Background(), 3, time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return fmt.Errorf("transient error %d", calls)
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestRetryPull_FailsAfterExhaustion(t *testing.T) {
	calls := 0
	sentinel := errors.New("ghcr boom")
	err := retryPull(context.Background(), 3, time.Millisecond, func() error {
		calls++
		return sentinel
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "final error should wrap the last op error")
	assert.Equal(t, 3, calls)
}

func TestRetryPull_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := retryPull(ctx, 5, 10*time.Second, func() error {
		calls++
		if calls == 1 {
			cancel()
		}
		return errors.New("boom")
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls, "should not invoke op again after ctx cancellation")
}
