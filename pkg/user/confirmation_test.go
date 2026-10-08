package user

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app/grpc/tx"
	"github.com/cometbft/cometbft/rpc/core"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

type confirmationBatch struct {
	request *tx.TxStatusBatchRequest
	reply   chan *tx.TxStatusBatchResponse
	height  chan int64
}

type confirmationStream struct {
	heights chan int64
	err     chan error
}

type confirmationServer struct {
	tx.UnimplementedTxServer
	coregrpc.UnimplementedBlockAPIServer
	batches chan confirmationBatch
	streams chan confirmationStream
}

func (s *confirmationServer) TxStatusBatch(ctx context.Context, req *tx.TxStatusBatchRequest) (*tx.TxStatusBatchResponse, error) {
	call := confirmationBatch{req, make(chan *tx.TxStatusBatchResponse, 1), make(chan int64, 1)}
	s.batches <- call
	select {
	case response := <-call.reply:
		if height := <-call.height; height >= 0 {
			if err := grpc.SetHeader(ctx, metadata.Pairs(grpctypes.GRPCBlockHeightHeader, strconv.FormatInt(height, 10))); err != nil {
				return nil, err
			}
		}
		return response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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

func newConfirmationTestClient(t *testing.T, register ...func(*grpc.Server)) (*TxClient, *confirmationServer) {
	t.Helper()
	s := &confirmationServer{batches: make(chan confirmationBatch, 128), streams: make(chan confirmationStream, 10)}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	tx.RegisterTxServer(server, s)
	coregrpc.RegisterBlockAPIServer(server, s)
	for _, fn := range register {
		fn(server)
	}
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///confirmation", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}))
	require.NoError(t, err)
	client := &TxClient{conns: []*grpc.ClientConn{conn}, txTracker: make(map[string]txInfo)}
	t.Cleanup(func() { client.CloseConfirmations(); _ = conn.Close(); server.Stop(); _ = listener.Close() })
	return client, s
}

func TestConfirmationInitialResults(t *testing.T) {
	for _, name := range []string{"committed", "execution failure", "wrong hash", "missing status", "invalid height", "invalid metadata", "missing metadata"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := newConfirmationTestClient(t)
				hash := fmt.Sprintf("%064X", 1)
				client.txTracker[hash] = txInfo{}
				result := confirmAsync(t.Context(), client, hash)
				call := <-server.batches
				status := &tx.TxStatusResponse{Status: core.TxStatusCommitted, Height: 10, ExecutionCode: 0, GasUsed: 12, GasWanted: 15}
				item := &tx.TxStatusResult{TxHash: hash, Status: status}
				height := int64(10)
				switch name {
				case "execution failure":
					status.ExecutionCode, status.Error, status.Codespace = 7, "failed execution", "test"
				case "wrong hash":
					item.TxHash = fmt.Sprintf("%064X", 2)
				case "missing status":
					item.Status = nil
				case "invalid height":
					status.Height = 0
				case "invalid metadata":
					height = 0
				case "missing metadata":
					height = -1
				}
				call.height <- height
				call.reply <- &tx.TxStatusBatchResponse{Statuses: []*tx.TxStatusResult{item}}
				got := <-result
				if name == "committed" {
					require.NoError(t, got.err)
					require.Equal(t, int64(10), got.response.Height)
				} else {
					require.Error(t, got.err)
					require.Nil(t, got.response)
				}
				if name == "execution failure" {
					var execution *ExecutionError
					require.ErrorAs(t, got.err, &execution)
					require.Equal(t, &ExecutionError{TxHash: hash, Code: 7, ErrorLog: "failed execution", Codespace: "test", GasUsed: 12, GasWanted: 15}, execution)
				}
			})
		})
	}
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

func replyConfirmation(call confirmationBatch, height int64, status *tx.TxStatusResponse) {
	response := &tx.TxStatusBatchResponse{}
	for _, hash := range call.request.TxIds {
		response.Statuses = append(response.Statuses, &tx.TxStatusResult{TxHash: hash, Status: status})
	}
	call.height <- height
	call.reply <- response
}

func TestConfirmationCommitBarrier(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := newConfirmationTestClient(t)
		hash := fmt.Sprintf("%064X", 1)
		result := confirmAsync(t.Context(), client, hash)
		stream := <-server.streams
		committed := &tx.TxStatusResponse{Status: core.TxStatusCommitted, Height: 10, GasUsed: 12, GasWanted: 15, Signers: []string{"signer"}}
		replyConfirmation(<-server.batches, 9, committed)
		synctest.Wait()
		select {
		case <-result:
			t.Fatal("indexed transaction acknowledged before application commit")
		default:
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		require.Empty(t, server.batches, "idle subscription must not poll")
		stream.heights <- 10
		replyConfirmation(<-server.batches, 10, committed)
		got := <-result
		require.NoError(t, got.err)
		require.Equal(t, &TxResponse{Height: 10, TxHash: hash, GasUsed: 12, GasWanted: 15, Signers: []string{"signer"}}, got.response)
		synctest.Wait()
		client.confirmationMu.Lock()
		require.Nil(t, client.confirmations, "last waiter must release the observer")
		client.confirmationMu.Unlock()
	})
}
