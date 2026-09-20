package user

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app/grpc/tx"
	"github.com/cometbft/cometbft/rpc/core"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func pendingStatus() *tx.TxStatusResponse { return &tx.TxStatusResponse{Status: core.TxStatusPending} }
func committedStatus(height int64) *tx.TxStatusResponse {
	return &tx.TxStatusResponse{Status: core.TxStatusCommitted, Height: height}
}

func TestConfirmationRegistrationRaces(t *testing.T) {
	for _, beforeRead := range []bool{true, false} {
		t.Run(fmt.Sprint(beforeRead), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := newConfirmationTestClient(t)
				result := confirmAsync(t.Context(), client, fmt.Sprintf("%064X", 1))
				stream, initial := <-server.streams, <-server.batches
				stream.heights <- 10
				synctest.Wait()
				if beforeRead {
					replyConfirmation(initial, 10, committedStatus(10))
				} else {
					// A height arriving during an older status read must cause another read.
					replyConfirmation(initial, 9, pendingStatus())
					replyConfirmation(<-server.batches, 10, committedStatus(10))
				}
				require.NoError(t, (<-result).err)
			})
		})
	}
}

func TestConfirmationWaiterLimitAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		hash := fmt.Sprintf("%064X", 1)
		ctx, cancel := context.WithCancel(t.Context())
		canceled := confirmAsync(ctx, client, hash)
		stream, initial := <-server.streams, <-server.batches
		results := make([]<-chan confirmationResult, maxConfirmationWaiters-1)
		for i := range results {
			results[i] = confirmAsync(t.Context(), client, hash)
		}
		synctest.Wait()
		_, err := client.ConfirmTxSubscription(t.Context(), hash)
		require.ErrorContains(t, err, "waiter limit")
		cancel()
		require.ErrorIs(t, (<-canceled).err, context.Canceled)
		replyConfirmation(initial, 9, pendingStatus())
		synctest.Wait()
		require.Empty(t, server.batches)
		require.Empty(t, server.streams, "callers share one subscription")
		stream.heights <- 10
		replyConfirmation(<-server.batches, 10, committedStatus(10))
		for _, result := range results {
			require.NoError(t, (<-result).err)
		}
		synctest.Wait()
		require.Nil(t, client.confirmations)
	})
}

func TestConfirmationBatchesAndDuplicateHeights(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		results := make([]<-chan confirmationResult, 45)
		for i := range results {
			results[i] = confirmAsync(t.Context(), client, fmt.Sprintf("%064X", i+1))
		}
		stream := <-server.streams
		for {
			synctest.Wait()
			select {
			case call := <-server.batches:
				require.LessOrEqual(t, len(call.request.TxIds), 20)
				replyConfirmation(call, 9, pendingStatus())
			default:
				goto registered
			}
		}
	registered:
		stream.heights <- 9
		for range 3 {
			replyConfirmation(<-server.batches, 9, pendingStatus())
		}
		synctest.Wait()
		stream.heights <- 9
		stream.heights <- 8
		synctest.Wait()
		require.Empty(t, server.batches)
		stream.heights <- 10
		for range 3 {
			call := <-server.batches
			require.LessOrEqual(t, len(call.request.TxIds), 20)
			replyConfirmation(call, 10, committedStatus(10))
		}
		for _, result := range results {
			require.NoError(t, (<-result).err)
		}
	})
}

func TestConfirmationReconnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		result := confirmAsync(t.Context(), client, fmt.Sprintf("%064X", 1))
		stream := <-server.streams
		replyConfirmation(<-server.batches, 9, pendingStatus())
		stream.err <- status.Error(codes.Unavailable, "disconnected during commit")
		<-server.streams
		// Reconciliation covers a commit missed while disconnected, without another block.
		replyConfirmation(<-server.batches, 10, committedStatus(10))
		require.NoError(t, (<-result).err)
	})
}

func TestConfirmationSilence(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := newConfirmationTestClient(t)
				result := confirmAsync(t.Context(), client, fmt.Sprintf("%064X", 1))
				<-server.streams
				replyConfirmation(<-server.batches, 9, pendingStatus())
				synctest.Wait()
				time.Sleep(30 * time.Second)
				last := <-server.batches
				if committed {
					replyConfirmation(last, 10, committedStatus(10))
					require.NoError(t, (<-result).err)
				} else {
					replyConfirmation(last, 9, pendingStatus())
					require.ErrorContains(t, (<-result).err, "no committed height notifications")
				}
				synctest.Wait()
				time.Sleep(time.Minute)
				require.Empty(t, server.batches, "terminal reconciliation must not become polling")
				require.Nil(t, client.confirmations)
			})
		})
	}
}

func TestConfirmationShutdown(t *testing.T) {
	for _, closeClient := range []bool{false, true} {
		t.Run(fmt.Sprint(closeClient), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := newConfirmationTestClient(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				client.confirmationContext = ctx
				result := confirmAsync(t.Context(), client, fmt.Sprintf("%064X", 1))
				<-server.batches // shutdown must cancel a blocked status RPC
				if closeClient {
					client.CloseConfirmations()
				} else {
					cancel()
				}
				require.Error(t, (<-result).err)
				_, err := client.ConfirmTxSubscription(t.Context(), fmt.Sprintf("%064X", 2))
				require.Error(t, err)
				synctest.Wait()
				require.Nil(t, client.confirmations)
			})
		})
	}
}

func TestConfirmationSubscriptionFailure(t *testing.T) {
	for _, code := range []codes.Code{codes.Unavailable, codes.Unimplemented} {
		t.Run(code.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := newConfirmationTestClient(t)
				result := confirmAsync(t.Context(), client, fmt.Sprintf("%064X", 1))
				attempts := 1
				if code == codes.Unavailable {
					attempts = 4
				}
				for range attempts {
					stream := <-server.streams
					replyConfirmation(<-server.batches, 9, pendingStatus())
					synctest.Wait()
					stream.err <- status.Error(code, "subscription failed")
				}
				require.ErrorContains(t, (<-result).err, "height subscription unavailable")
				synctest.Wait()
				require.Empty(t, server.streams)
			})
		})
	}
}

type confirmationBroadcast struct {
	sdktx.UnimplementedServiceServer
	requests chan *sdktx.BroadcastTxRequest
	code     uint32
}

func (s *confirmationBroadcast) BroadcastTx(_ context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
	s.requests <- req
	return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{Code: s.code}}, nil
}

func TestConfirmationRecovery(t *testing.T) {
	for _, test := range []struct {
		state string
		code  uint32
	}{{core.TxStatusRejected, 0}, {core.TxStatusEvicted, 0}, {core.TxStatusEvicted, 32}} {
		state := test.state
		t.Run(fmt.Sprintf("%s/%d", state, test.code), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				broadcast := &confirmationBroadcast{requests: make(chan *sdktx.BroadcastTxRequest, 2), code: test.code}
				client, server := newConfirmationTestClient(t, func(s *grpc.Server) { sdktx.RegisterServiceServer(s, broadcast) })
				hash := fmt.Sprintf("%064X", 1)
				client.signer = &Signer{accounts: map[string]*Account{"signer": NewAccount("signer", 1, 9)}}
				client.txTracker[hash] = txInfo{sequence: 8, signer: "signer", txBytes: []byte("signed transaction")}
				result := confirmAsync(t.Context(), client, hash)
				stream := <-server.streams
				replyConfirmation(<-server.batches, 9, &tx.TxStatusResponse{Status: state, Error: "rejected"})
				if state == core.TxStatusRejected {
					require.ErrorContains(t, (<-result).err, "rejected by the node")
					require.Equal(t, uint64(8), client.signer.Account("signer").Sequence())
				} else {
					require.Equal(t, []byte("signed transaction"), (<-broadcast.requests).TxBytes)
					synctest.Wait()
					stream.heights <- 10
					next := pendingStatus()
					if test.code != 0 {
						next.Status = core.TxStatusEvicted
					}
					replyConfirmation(<-server.batches, 10, next)
					synctest.Wait()
					require.Empty(t, broadcast.requests, "do not rebroadcast while pending or after a broadcast error")
					stream.heights <- 11
					replyConfirmation(<-server.batches, 11, committedStatus(11))
					require.NoError(t, (<-result).err)
				}
				synctest.Wait()
				require.Empty(t, client.txTracker)
			})
		})
	}
}

func TestConfirmationDeadlines(t *testing.T) {
	for _, callerDeadline := range []bool{false, true} {
		t.Run(fmt.Sprint(callerDeadline), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := newConfirmationTestClient(t)
				ctx := t.Context()
				if callerDeadline {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, time.Second)
					defer cancel()
				}
				result := confirmAsync(ctx, client, fmt.Sprintf("%064X", 1))
				<-server.batches
				got := <-result
				if callerDeadline {
					require.ErrorIs(t, got.err, context.DeadlineExceeded)
				} else {
					require.Equal(t, codes.DeadlineExceeded, status.Code(got.err))
				}
				synctest.Wait()
				require.Nil(t, client.confirmations)
				require.Empty(t, server.batches)
			})
		})
	}
}

func TestConfirmationSlowStatusConsumer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		result := confirmAsync(t.Context(), client, fmt.Sprintf("%064X", 1))
		stream, initial := <-server.streams, <-server.batches
		for height := int64(1); height <= 100; height++ {
			stream.heights <- height
		}
		synctest.Wait()
		replyConfirmation(initial, 1, pendingStatus())
		replyConfirmation(<-server.batches, 100, committedStatus(100))
		require.NoError(t, (<-result).err)
		synctest.Wait()
		require.Empty(t, server.batches)
	})
}

func TestConfirmationRejectionWithoutBlocks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		hash := fmt.Sprintf("%064X", 1)
		client.signer = &Signer{accounts: map[string]*Account{"signer": NewAccount("signer", 1, 9)}}
		client.txTracker[hash] = txInfo{signer: "signer", sequence: 8}
		result := confirmAsync(t.Context(), client, hash)
		replyConfirmation(<-server.batches, 9, pendingStatus())
		synctest.Wait()
		time.Sleep(30 * time.Second)
		replyConfirmation(<-server.batches, 9, &tx.TxStatusResponse{Status: core.TxStatusRejected})
		require.ErrorContains(t, (<-result).err, "rejected by the node")
		require.Equal(t, uint64(8), client.signer.Account("signer").Sequence())
	})
}
