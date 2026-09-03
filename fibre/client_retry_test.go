package fibre

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestRetryAfter(t *testing.T) {
	withRetryInfo := func(d time.Duration) error {
		st, err := status.New(grpccodes.ResourceExhausted, "storage budget exceeded").
			WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(d)})
		require.NoError(t, err)
		return st.Err()
	}

	tests := map[string]struct {
		err  error
		want time.Duration
	}{
		"nil error is not retryable":             {nil, 0},
		"plain error is not retryable":           {errors.New("boom"), 0},
		"non-ResourceExhausted is not retryable": {status.Error(grpccodes.Internal, "x"), 0},
		"ResourceExhausted without hint":         {status.Error(grpccodes.ResourceExhausted, "full"), defaultRetryDelay},
		"ResourceExhausted with hint":            {withRetryInfo(3 * time.Second), 3 * time.Second},
		"ResourceExhausted with zero hint":       {withRetryInfo(0), defaultRetryDelay},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.want, retryAfter(tt.err))
		})
	}
}

func TestRetryPFFBroadcast(t *testing.T) {
	histErr := errors.New("broadcast tx error: validator signature validation failed: failed to get historical validator set at height 27: no historical info found")
	otherErr := errors.New("insufficient fees")

	t.Run("success on first attempt", func(t *testing.T) {
		calls := 0
		resp, err := retryPFFBroadcast(t.Context(), func(context.Context) (*sdk.TxResponse, error) {
			calls++
			return &sdk.TxResponse{TxHash: "AB"}, nil
		})
		require.NoError(t, err)
		require.Equal(t, "AB", resp.TxHash)
		require.Equal(t, 1, calls)
	})

	t.Run("non-retryable error returns immediately", func(t *testing.T) {
		calls := 0
		_, err := retryPFFBroadcast(t.Context(), func(context.Context) (*sdk.TxResponse, error) {
			calls++
			return nil, otherErr
		})
		require.ErrorIs(t, err, otherErr)
		require.Equal(t, 1, calls)
	})

	t.Run("re-broadcasts until the height is committed", func(t *testing.T) {
		calls := 0
		resp, err := retryPFFBroadcast(t.Context(), func(context.Context) (*sdk.TxResponse, error) {
			calls++
			if calls < 3 {
				return nil, histErr
			}
			return &sdk.TxResponse{TxHash: "CD"}, nil
		})
		require.NoError(t, err)
		require.Equal(t, "CD", resp.TxHash)
		require.Equal(t, 3, calls)
	})

	t.Run("gives up after the attempt budget", func(t *testing.T) {
		calls := 0
		_, err := retryPFFBroadcast(t.Context(), func(context.Context) (*sdk.TxResponse, error) {
			calls++
			return nil, histErr
		})
		require.ErrorIs(t, err, histErr)
		require.Equal(t, pffBroadcastAttempts, calls)
	})

	t.Run("stops when the context ends", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		calls := 0
		_, err := retryPFFBroadcast(ctx, func(context.Context) (*sdk.TxResponse, error) {
			calls++
			return nil, histErr
		})
		require.ErrorIs(t, err, histErr)
		require.Equal(t, 1, calls)
	})
}
