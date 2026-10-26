package user

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app/grpc/tx"
	"github.com/cometbft/cometbft/rpc/core"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// confirmationServer serves TxStatus, TxStatusBatch and SubscribeNewHeights from
// in-memory state. Unknown hashes are pending.
type confirmationServer struct {
	tx.UnimplementedTxServer
	coregrpc.UnimplementedBlockAPIServer

	mu         sync.Mutex
	height     int64 // committed application height; -1 omits the header
	statuses   map[string]*tx.TxStatusResponse
	batchDelay time.Duration
	batchHash  string // when set, batches answer with this hash
	single     int
	batches    int

	streams chan confirmationStream
}

type confirmationStream struct {
	heights chan int64
	err     chan error
}

func (s *confirmationServer) lookup(ctx context.Context, hash string) (*tx.TxStatusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.height >= 0 {
		if err := grpc.SetHeader(ctx, metadata.Pairs(grpctypes.GRPCBlockHeightHeader, strconv.FormatInt(s.height, 10))); err != nil {
			return nil, err
		}
	}
	if st, ok := s.statuses[hash]; ok {
		return st, nil
	}
	return &tx.TxStatusResponse{Status: core.TxStatusPending}, nil
}

func (s *confirmationServer) TxStatus(ctx context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
	s.mu.Lock()
	s.single++
	s.mu.Unlock()
	return s.lookup(ctx, req.TxId)
}

func (s *confirmationServer) TxStatusBatch(ctx context.Context, req *tx.TxStatusBatchRequest) (*tx.TxStatusBatchResponse, error) {
	s.mu.Lock()
	s.batches++
	delay, batchHash := s.batchDelay, s.batchHash
	s.mu.Unlock()
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	resp := &tx.TxStatusBatchResponse{}
	for _, hash := range req.TxIds {
		st, err := s.lookup(ctx, hash)
		if err != nil {
			return nil, err
		}
		if batchHash != "" {
			hash = batchHash
		}
		resp.Statuses = append(resp.Statuses, &tx.TxStatusResult{TxHash: hash, Status: st})
	}
	return resp, nil
}

func (s *confirmationServer) SubscribeNewHeights(_ *coregrpc.SubscribeNewHeightsRequest, stream coregrpc.BlockAPI_SubscribeNewHeightsServer) error {
	control := confirmationStream{make(chan int64, 10), make(chan error, 1)}
	s.streams <- control
	for {
		select {
		case height := <-control.heights:
			if err := stream.Send(&coregrpc.SubscribeNewHeightsResponse{Height: height}); err != nil {
				return err
			}
		case err := <-control.err:
			return err
		case <-stream.Context().Done():
			return nil
		}
	}
}

// commit marks hashes committed at height and advances the application height to it.
func (s *confirmationServer) commit(height int64, hashes ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.height = height
	for _, hash := range hashes {
		s.statuses[hash] = &tx.TxStatusResponse{Status: core.TxStatusCommitted, Height: height, GasUsed: 12, GasWanted: 15, Signers: []string{"signer"}}
	}
}

func (s *confirmationServer) calls() (single, batches int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.single, s.batches
}

func newConfirmationTestClient(t *testing.T) (*TxClient, *confirmationServer) {
	t.Helper()
	s := &confirmationServer{height: 1, statuses: make(map[string]*tx.TxStatusResponse), streams: make(chan confirmationStream, 10)}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	tx.RegisterTxServer(server, s)
	coregrpc.RegisterBlockAPIServer(server, s)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///confirmation", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}))
	require.NoError(t, err)
	client := &TxClient{conns: []*grpc.ClientConn{conn}, txTracker: make(map[string]txInfo)}
	t.Cleanup(func() { _ = conn.Close(); server.Stop(); _ = listener.Close() })
	return client, s
}

type confirmationResult struct {
	response *TxResponse
	err      error
}

func confirmAsync(ctx context.Context, client *TxClient, hash string) <-chan confirmationResult {
	result := make(chan confirmationResult, 1)
	go func() {
		resp, err := client.ConfirmTxSubscription(ctx, hash)
		result <- confirmationResult{resp, err}
	}()
	return result
}

func requireWaiting(t *testing.T, result <-chan confirmationResult) {
	t.Helper()
	synctest.Wait()
	select {
	case got := <-result:
		t.Fatalf("confirmation returned early: %+v", got)
	default:
	}
}

func requireFeedStopped(t *testing.T, client *TxClient) {
	t.Helper()
	synctest.Wait()
	client.confirmationMu.Lock()
	defer client.confirmationMu.Unlock()
	require.Nil(t, client.statusFeed, "last waiter must stop the feed")
}

func TestConfirmationInitialCheck(t *testing.T) {
	hash := fmt.Sprintf("%064X", 1)
	for name, tc := range map[string]struct {
		height  int64
		status  *tx.TxStatusResponse
		wantErr string
	}{
		"committed":         {height: 10, status: &tx.TxStatusResponse{Status: core.TxStatusCommitted, Height: 10, GasUsed: 12}},
		"execution failure": {height: 10, status: &tx.TxStatusResponse{Status: core.TxStatusCommitted, Height: 10, ExecutionCode: 7, Error: "failed execution", Codespace: "test", GasUsed: 12, GasWanted: 15}, wantErr: "failed execution"},
		"invalid height":    {height: 10, status: &tx.TxStatusResponse{Status: core.TxStatusCommitted}, wantErr: "invalid transaction inclusion height"},
		"invalid metadata":  {height: 0, wantErr: "invalid committed application height metadata"},
		"missing metadata":  {height: -1, wantErr: "requires committed application height metadata"},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := newConfirmationTestClient(t)
				client.txTracker[hash] = txInfo{}
				server.height = tc.height
				if tc.status != nil {
					server.statuses[hash] = tc.status
				}
				got := <-confirmAsync(t.Context(), client, hash)
				if tc.wantErr == "" {
					require.NoError(t, got.err)
					require.Equal(t, int64(10), got.response.Height)
				} else {
					require.ErrorContains(t, got.err, tc.wantErr)
					require.Nil(t, got.response)
				}
				if name == "execution failure" {
					var execution *ExecutionError
					require.ErrorAs(t, got.err, &execution)
					require.Equal(t, &ExecutionError{TxHash: hash, Code: 7, ErrorLog: "failed execution", Codespace: "test", GasUsed: 12, GasWanted: 15}, execution)
				}
				requireFeedStopped(t, client)
			})
		})
	}
}

func TestConfirmationInvalidHash(t *testing.T) {
	client, _ := newConfirmationTestClient(t)
	for _, hash := range []string{"", "ABCD", fmt.Sprintf("%064X", 1)[:63] + "Z"} {
		_, err := client.ConfirmTxSubscription(t.Context(), hash)
		require.ErrorContains(t, err, "32 hex-encoded bytes")
	}
}

// A committed result is reported only once the application height covers it,
// and the feed checks on new heights rather than polling.
func TestConfirmationCommitBarrier(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		hash := fmt.Sprintf("%064X", 1)
		server.commit(10, hash)
		server.height = 9
		result := confirmAsync(t.Context(), client, hash)
		stream := <-server.streams
		requireWaiting(t, result)

		time.Sleep(10 * time.Second)
		requireWaiting(t, result)
		single, batches := server.calls()
		require.Equal(t, 1, single, "only the initial direct check")
		require.Zero(t, batches, "idle subscription must not poll")

		server.commit(10)
		stream.heights <- 10
		got := <-result
		require.NoError(t, got.err)
		require.Equal(t, &TxResponse{Height: 10, TxHash: hash, GasUsed: 12, GasWanted: 15, Signers: []string{"signer"}}, got.response)
		requireFeedStopped(t, client)
	})
}

// Regression: a full refresh used to share one 5s timeout across all batches,
// so 5 sequential 1.2s batches failed every waiter.
func TestConfirmationManyWaitersSlowBatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		server.batchDelay = 1200 * time.Millisecond
		const waiters = 5 * confirmationBatchSize
		hashes := make([]string, waiters)
		results := make([]<-chan confirmationResult, waiters)
		for i := range hashes {
			hashes[i] = fmt.Sprintf("%064X", i+1)
			results[i] = confirmAsync(t.Context(), client, hashes[i])
		}
		stream := <-server.streams
		synctest.Wait()

		server.commit(2, hashes...)
		stream.heights <- 2
		for i, result := range results {
			got := <-result
			require.NoError(t, got.err, "waiter %d", i)
		}
		_, batches := server.calls()
		require.Equal(t, waiters/confirmationBatchSize, batches)
		requireFeedStopped(t, client)
	})
}

// Regression: the stream used to fail every waiter after its 4th drop over its
// whole lifetime. Reconnects are now unbounded while anyone is waiting.
func TestConfirmationStreamReconnects(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		hash := fmt.Sprintf("%064X", 1)
		result := confirmAsync(t.Context(), client, hash)
		stream := <-server.streams
		for range 10 {
			stream.heights <- 2
			synctest.Wait()
			stream.err <- status.Error(codes.Unavailable, "dropped")
			stream = <-server.streams
		}
		requireWaiting(t, result)

		server.commit(3, hash)
		stream.heights <- 3
		require.NoError(t, (<-result).err)
	})
}

// Bad batch responses are skipped instead of failing waiters, and a waiter whose
// status the feed can't deliver falls back to checking directly.
func TestConfirmationFallbackOnBadBatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		hash := fmt.Sprintf("%064X", 1)
		server.batchHash = fmt.Sprintf("%064X", 2)
		result := confirmAsync(t.Context(), client, hash)
		stream := <-server.streams
		synctest.Wait()

		server.commit(2, hash)
		stream.heights <- 2
		requireWaiting(t, result)

		time.Sleep(confirmationFallbackInterval)
		got := <-result
		require.NoError(t, got.err)
		require.Equal(t, int64(2), got.response.Height)
	})
}

// Waiters on the same hash share one batch entry and all get the result;
// a cancelled waiter leaves the others running.
func TestConfirmationSharedHash(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		hash := fmt.Sprintf("%064X", 1)
		cancelled, cancel := context.WithCancel(t.Context())
		first := confirmAsync(cancelled, client, hash)
		second := confirmAsync(t.Context(), client, hash)
		stream := <-server.streams
		synctest.Wait()

		cancel()
		require.ErrorIs(t, (<-first).err, context.Canceled)
		requireWaiting(t, second)

		server.commit(2, hash)
		stream.heights <- 2
		require.NoError(t, (<-second).err)
		requireFeedStopped(t, client)
	})
}
