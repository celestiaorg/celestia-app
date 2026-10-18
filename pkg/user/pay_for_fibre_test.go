package user

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

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
