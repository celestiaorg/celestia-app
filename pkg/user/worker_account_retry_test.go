package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsAccountNotFound(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "grpc NotFound", err: status.Error(codes.NotFound, "account celestia1... not found"), want: true},
		{name: "plain not found string", err: errors.New("account celestia1... not found"), want: true},
		{name: "unrelated grpc error", err: status.Error(codes.Internal, "boom"), want: false},
		{name: "unrelated error", err: errors.New("connection refused"), want: false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isAccountNotFound(tc.err))
		})
	}
}

func TestQueryAccountWithRetry(t *testing.T) {
	notFound := status.Error(codes.NotFound, "account not found")

	t.Run("succeeds immediately", func(t *testing.T) {
		calls := 0
		accNum, seqNum, err := queryAccountWithRetry(context.Background(), 5, time.Millisecond, func() (uint64, uint64, error) {
			calls++
			return 7, 3, nil
		})
		require.NoError(t, err)
		require.Equal(t, uint64(7), accNum)
		require.Equal(t, uint64(3), seqNum)
		require.Equal(t, 1, calls)
	})

	t.Run("succeeds after transient not found", func(t *testing.T) {
		calls := 0
		accNum, seqNum, err := queryAccountWithRetry(context.Background(), 5, time.Millisecond, func() (uint64, uint64, error) {
			calls++
			if calls < 3 {
				return 0, 0, notFound
			}
			return 9, 1, nil
		})
		require.NoError(t, err)
		require.Equal(t, uint64(9), accNum)
		require.Equal(t, uint64(1), seqNum)
		require.Equal(t, 3, calls)
	})

	t.Run("gives up after exhausting attempts", func(t *testing.T) {
		calls := 0
		_, _, err := queryAccountWithRetry(context.Background(), 3, time.Millisecond, func() (uint64, uint64, error) {
			calls++
			return 0, 0, notFound
		})
		require.Error(t, err)
		require.Equal(t, notFound, err)
		require.Equal(t, 3, calls)
	})

	t.Run("does not retry on other errors", func(t *testing.T) {
		calls := 0
		other := errors.New("connection refused")
		_, _, err := queryAccountWithRetry(context.Background(), 5, time.Millisecond, func() (uint64, uint64, error) {
			calls++
			return 0, 0, other
		})
		require.Equal(t, other, err)
		require.Equal(t, 1, calls)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := 0
		_, _, err := queryAccountWithRetry(ctx, 5, time.Hour, func() (uint64, uint64, error) {
			calls++
			return 0, 0, notFound
		})
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, calls)
	})
}
