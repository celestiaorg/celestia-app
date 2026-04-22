package v3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v9/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	"github.com/celestiaorg/celestia-app/v9/pkg/user/v2"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/rpc/core"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// loadTestServer is a mock gRPC server that tracks broadcast sequences to detect
// sequence mismatches and duplicate submissions.
type loadTestServer struct {
	sdktx.UnimplementedServiceServer
	tx.UnimplementedTxServer
	gasestimation.UnimplementedGasEstimatorServer

	mu             sync.Mutex
	broadcastedTxs map[string]bool   // txHash -> true (detect duplicates)
	sequencesSeen  map[uint64]string // sequence -> txHash (detect conflicts)
	totalBroadcast atomic.Int64
	totalConfirmed atomic.Int64
	seqMismatches  atomic.Int64
	evictions      atomic.Int64

	// committed tracks which txs have been "committed" (after a brief delay).
	committed sync.Map // txHash -> *tx.TxStatusResponse
}

func (s *loadTestServer) BroadcastTx(_ context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
	sum := sha256.Sum256(req.TxBytes)
	txHash := hex.EncodeToString(sum[:])

	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalBroadcast.Add(1)

	// Check for duplicate
	if s.broadcastedTxs[txHash] {
		return &sdktx.BroadcastTxResponse{
			TxResponse: &sdk.TxResponse{
				TxHash: txHash,
				Code:   abci.CodeTypeOK,
			},
		}, nil
	}

	s.broadcastedTxs[txHash] = true

	// Simulate commitment after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.committed.Store(txHash, &tx.TxStatusResponse{
			Status:        core.TxStatusCommitted,
			Height:        100,
			ExecutionCode: abci.CodeTypeOK,
		})
		s.totalConfirmed.Add(1)
	}()

	return &sdktx.BroadcastTxResponse{
		TxResponse: &sdk.TxResponse{
			TxHash: txHash,
			Code:   abci.CodeTypeOK,
		},
	}, nil
}

func (s *loadTestServer) TxStatus(_ context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
	if resp, ok := s.committed.Load(req.TxId); ok {
		return resp.(*tx.TxStatusResponse), nil
	}
	return &tx.TxStatusResponse{
		Status: core.TxStatusPending,
	}, nil
}

func (s *loadTestServer) TxStatusBatch(_ context.Context, req *tx.TxStatusBatchRequest) (*tx.TxStatusBatchResponse, error) {
	results := make([]*tx.TxStatusResult, 0, len(req.TxIds))
	for _, txID := range req.TxIds {
		var resp *tx.TxStatusResponse
		if r, ok := s.committed.Load(txID); ok {
			resp = r.(*tx.TxStatusResponse)
		} else {
			resp = &tx.TxStatusResponse{Status: core.TxStatusPending}
		}
		results = append(results, &tx.TxStatusResult{
			TxHash: txID,
			Status: resp,
		})
	}
	return &tx.TxStatusBatchResponse{Statuses: results}, nil
}

func (s *loadTestServer) EstimateGasPriceAndUsage(_ context.Context, _ *gasestimation.EstimateGasPriceAndUsageRequest) (*gasestimation.EstimateGasPriceAndUsageResponse, error) {
	return &gasestimation.EstimateGasPriceAndUsageResponse{
		EstimatedGasPrice: 0.002,
		EstimatedGasUsed:  70000,
	}, nil
}

// setupLoadTest creates a TxClientV3 connected to a mock gRPC server.
func setupLoadTest(t *testing.T) (*TxClientV3, *loadTestServer) {
	t.Helper()

	server := &loadTestServer{
		broadcastedTxs: make(map[string]bool),
		sequencesSeen:  make(map[uint64]string),
	}

	// Set up in-memory gRPC server.
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	sdktx.RegisterServiceServer(s, server)
	tx.RegisterTxServer(s, server)
	gasestimation.RegisterGasEstimatorServer(s, server)

	go func() {
		if err := s.Serve(lis); err != nil {
			t.Logf("server exited: %v", err)
		}
	}()
	t.Cleanup(s.GracefulStop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	// Create keyring with a test account.
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(encCfg.Codec)
	_, _, err = kr.NewMnemonic("test-account", keyring.English, "", "", hd.Secp256k1)
	require.NoError(t, err)

	signer, err := user.NewSigner(
		kr,
		encCfg.TxConfig,
		"test-chain",
		user.NewAccount("test-account", 0, 1),
	)
	require.NoError(t, err)

	v1Client, err := user.NewTxClient(
		encCfg.Codec,
		signer,
		conn,
		encCfg.InterfaceRegistry,
		user.WithPollTime(50*time.Millisecond),
	)
	require.NoError(t, err)

	v2Client := v2.Wrapv1TxClient(v1Client)
	v3Client, err := NewTxClientV3(context.Background(), v2Client, WithQueueSize(1000))
	require.NoError(t, err)
	t.Cleanup(v3Client.Close)

	return v3Client, server
}

// TestLoadConcurrentAddTx spawns many goroutines submitting txs concurrently
// and verifies no sequence mismatches, no evictions, and all txs confirm.
func TestLoadConcurrentAddTx(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	client, server := setupLoadTest(t)

	const numGoroutines = 100
	const txsPerGoroutine = 10
	totalTxs := numGoroutines * txsPerGoroutine

	var (
		wg        sync.WaitGroup
		confirmed atomic.Int64
		errCount  atomic.Int64
		handles   = make([]*TxHandle, 0, totalTxs)
		handlesMu sync.Mutex
	)

	// Sender address from the test account.
	senderAddr := client.DefaultAddress()

	// Spawn goroutines that each submit multiple txs.
	for g := range numGoroutines {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := range txsPerGoroutine {
				bankMsg := newBankSendMsg(senderAddr, senderAddr, 1)

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				handle, err := client.AddTx(ctx, []sdk.Msg{bankMsg})
				if err != nil {
					cancel()
					errCount.Add(1)
					t.Logf("goroutine %d tx %d: AddTx error: %v", goroutineID, i, err)
					continue
				}

				handlesMu.Lock()
				handles = append(handles, handle)
				handlesMu.Unlock()

				// Wait for confirmation in a separate goroutine.
				wg.Add(1)
				go func(h *TxHandle, gID, txID int, cancelFn context.CancelFunc) {
					defer wg.Done()
					defer cancelFn()

					_, err := h.Await(ctx)
					switch {
					case err == nil:
						confirmed.Add(1)
					case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
						errCount.Add(1)
						t.Logf("goroutine %d tx %d: timeout waiting for confirm", gID, txID)
					default:
						errCount.Add(1)
						t.Logf("goroutine %d tx %d: confirm error: %v", gID, txID, err)
					}
				}(handle, goroutineID, i, cancel)
			}
		}(g)
	}

	wg.Wait()

	t.Logf("Results: confirmed=%d errors=%d total=%d",
		confirmed.Load(), errCount.Load(), totalTxs)
	t.Logf("Server stats: broadcast=%d confirmed=%d mismatches=%d evictions=%d",
		server.totalBroadcast.Load(), server.totalConfirmed.Load(),
		server.seqMismatches.Load(), server.evictions.Load())

	assert.Equal(t, int64(0), server.seqMismatches.Load(), "should have zero sequence mismatches")
	assert.Equal(t, int64(0), server.evictions.Load(), "should have zero evictions")
	// All txs that were successfully enqueued should confirm.
	assert.Equal(t, int64(totalTxs)-errCount.Load(), confirmed.Load(),
		"all enqueued txs should confirm")
}

// BenchmarkAddTx measures throughput of the async pipeline.
func BenchmarkAddTx(b *testing.B) {
	server := &loadTestServer{
		broadcastedTxs: make(map[string]bool),
		sequencesSeen:  make(map[uint64]string),
	}

	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	sdktx.RegisterServiceServer(s, server)
	tx.RegisterTxServer(s, server)
	gasestimation.RegisterGasEstimatorServer(s, server)

	go func() {
		if err := s.Serve(lis); err != nil {
			b.Logf("server exited: %v", err)
		}
	}()
	defer s.GracefulStop()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(b, err)
	defer conn.Close()

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(encCfg.Codec)
	_, _, err = kr.NewMnemonic("bench-account", keyring.English, "", "", hd.Secp256k1)
	require.NoError(b, err)

	signer, err := user.NewSigner(kr, encCfg.TxConfig, "bench-chain",
		user.NewAccount("bench-account", 0, 1))
	require.NoError(b, err)

	v1Client, err := user.NewTxClient(encCfg.Codec, signer, conn, encCfg.InterfaceRegistry,
		user.WithPollTime(20*time.Millisecond))
	require.NoError(b, err)

	v2Client := v2.Wrapv1TxClient(v1Client)
	ctx := b.Context()

	v3Client, err := NewTxClientV3(ctx, v2Client)
	require.NoError(b, err)
	defer v3Client.Close()

	senderAddr := v3Client.DefaultAddress()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			msg := newBankSendMsg(senderAddr, senderAddr, 1)
			txCtx, txCancel := context.WithTimeout(ctx, 30*time.Second)
			handle, err := v3Client.AddTx(txCtx, []sdk.Msg{msg})
			if err != nil {
				txCancel()
				b.Logf("AddTx error: %v", err)
				continue
			}

			if _, err := handle.Await(txCtx); err != nil {
				b.Logf("await error: %v", err)
			}
			txCancel()
		}
	})
	b.StopTimer()

	b.Logf("Server: broadcast=%d confirmed=%d mismatches=%d",
		server.totalBroadcast.Load(), server.totalConfirmed.Load(),
		server.seqMismatches.Load())
}

// newBankSendMsg creates a simple bank send message for testing.
func newBankSendMsg(from, to sdk.AccAddress, amount int64) sdk.Msg {
	coins := sdk.NewCoins(sdk.NewInt64Coin("utia", amount))
	return &banktypes.MsgSend{
		FromAddress: from.String(),
		ToAddress:   to.String(),
		Amount:      coins,
	}
}
