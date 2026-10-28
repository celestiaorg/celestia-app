package user

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app/grpc/tx"
	"github.com/cometbft/cometbft/rpc/core"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A rejection reaches the waiter both through the feed and through its direct
// fallback check, and resets the signer's sequence.
func TestConfirmationRejection(t *testing.T) {
	for _, viaFeed := range []bool{true, false} {
		t.Run(fmt.Sprintf("feed=%v", viaFeed), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := newConfirmationTestClient(t)
				hash := fmt.Sprintf("%064X", 1)
				client.signer = &Signer{accounts: map[string]*Account{"signer": NewAccount("signer", 1, 9)}}
				client.txTracker[hash] = txInfo{signer: "signer", sequence: 8}
				result := confirmAsync(t.Context(), client, hash)
				stream := <-server.streams
				requireWaiting(t, result)

				server.mu.Lock()
				server.statuses[hash] = &tx.TxStatusResponse{Status: core.TxStatusRejected, Error: "rejected"}
				server.mu.Unlock()
				if viaFeed {
					stream.heights <- 2
				} else {
					time.Sleep(confirmationFallbackInterval)
				}
				require.ErrorContains(t, (<-result).err, "rejected by the node")
				require.Equal(t, uint64(8), client.signer.Account("signer").Sequence())
				require.Empty(t, client.txTracker)
				requireFeedStopped(t, client)
			})
		})
	}
}

// A caller deadline ends the wait, and the last waiter leaving cancels an
// in-flight batch.
func TestConfirmationCallerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		server.batchDelay = time.Minute
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		result := confirmAsync(ctx, client, fmt.Sprintf("%064X", 1))
		stream := <-server.streams
		stream.heights <- 2
		synctest.Wait()

		require.ErrorIs(t, (<-result).err, context.DeadlineExceeded)
		requireFeedStopped(t, client)
		_, batches := server.calls()
		require.Equal(t, 1, batches)
	})
}

// Without a height subscription, waiters still confirm by checking directly.
func TestConfirmationSubscriptionUnimplemented(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() {
			for {
				select {
				case stream := <-server.streams:
					stream.err <- status.Error(codes.Unimplemented, "no subscription")
				case <-ctx.Done():
					return
				}
			}
		}()
		hash := fmt.Sprintf("%064X", 1)
		result := confirmAsync(t.Context(), client, hash)
		requireWaiting(t, result)

		server.commit(2, hash)
		requireWaiting(t, result)
		time.Sleep(confirmationFallbackInterval)
		got := <-result
		require.NoError(t, got.err)
		require.Equal(t, int64(2), got.response.Height)
		_, batches := server.calls()
		require.Zero(t, batches)
		requireFeedStopped(t, client)
	})
}
