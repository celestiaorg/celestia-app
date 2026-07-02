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

func TestIsPortBindingError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "unrelated", err: errors.New("context deadline exceeded"), want: false},
		{
			name: "docker address already in use",
			err: errors.New("Error response from daemon: failed to set up container networking: " +
				"driver failed programming external connectivity on endpoint test-val-0 " +
				"(abc123): failed to listen on TCP socket: address already in use"),
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isPortBindingError(tc.err))
		})
	}
}

func TestRetryOnPortCollision_SucceedsOnFirstAttempt(t *testing.T) {
	calls := 0
	err := retryOnPortCollision(context.Background(), 3, time.Millisecond, func() error {
		calls++
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestRetryOnPortCollision_RetriesPortBindingErrors(t *testing.T) {
	calls := 0
	err := retryOnPortCollision(context.Background(), 3, time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return fmt.Errorf("attempt %d: failed to listen on TCP socket: address already in use", calls)
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestRetryOnPortCollision_DoesNotRetryOtherErrors(t *testing.T) {
	calls := 0
	sentinel := errors.New("genesis export failed")
	err := retryOnPortCollision(context.Background(), 3, time.Millisecond, func() error {
		calls++
		return sentinel
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 1, calls, "non-port-binding errors must not be retried")
}

func TestRetryOnPortCollision_FailsAfterExhaustion(t *testing.T) {
	calls := 0
	err := retryOnPortCollision(context.Background(), 3, time.Millisecond, func() error {
		calls++
		return errors.New("failed to listen on TCP socket: address already in use")
	})

	require.Error(t, err)
	assert.Equal(t, 3, calls)
}

func TestRetryOnPortCollision_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := retryOnPortCollision(ctx, 5, 10*time.Second, func() error {
		calls++
		if calls == 1 {
			cancel()
		}
		return errors.New("failed to listen on TCP socket: address already in use")
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls, "should not invoke op again after ctx cancellation")
}
