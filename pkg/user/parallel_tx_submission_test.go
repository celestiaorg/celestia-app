package user_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/go-square/v3/share"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/rpc/core"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func newMockTxClientWithCustomHandlers(t *testing.T, broadcastHandler func(context.Context, *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error), txStatusHandler func(context.Context, *tx.TxStatusRequest) (*tx.TxStatusResponse, error), workerAccounts []string) (*user.TxClient, *grpc.ClientConn) {
	t.Helper()

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(encCfg.Codec)

	accountNames := make([]string, 0, len(workerAccounts)+1)
	accountNames = append(accountNames, "master")
	accountNames = append(accountNames, workerAccounts...)

	for i, name := range accountNames {
		path := hd.CreateHDPath(sdktypes.CoinType, 0, uint32(i)).String()
		_, _, err := kr.NewMnemonic(name, keyring.English, path, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
		require.NoError(t, err)
	}

	accounts := make([]*user.Account, len(accountNames))
	for i, name := range accountNames {
		accounts[i] = user.NewAccount(name, uint64(i+1), 0)
	}

	signer, err := user.NewSigner(kr, encCfg.TxConfig, "test-chain", accounts...)
	require.NoError(t, err)

	options := []user.Option{
		user.WithPollTime(10 * time.Millisecond),
	}
	if len(workerAccounts) > 0 {
		// For mock tests, use WithTxWorkersNoInit since we're providing mock accounts
		option := user.WithTxWorkersNoInit(len(workerAccounts))
		options = append(options, option)
	}

	return setupTxClientWithMockGRPCServerAndSigner(t, make(map[string][]*tx.TxStatusResponse), broadcastHandler, txStatusHandler, encCfg, signer, options...)
}

func setupTxClientWithMockGRPCServerAndSigner(t *testing.T, responseSequences map[string][]*tx.TxStatusResponse, broadcastHandler func(context.Context, *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error), txStatusHandler func(context.Context, *tx.TxStatusRequest) (*tx.TxStatusResponse, error), encCfg encoding.Config, signer *user.Signer, opts ...user.Option) (*user.TxClient, *grpc.ClientConn) {
	t.Helper()

	mockServer := &mockTxServer{
		txStatusResponses:   responseSequences,
		txStatusCallCounts:  make(map[string]int),
		broadcastCallCounts: make(map[string]int),
	}

	// Set handlers: use provided custom handlers or default to original behavior
	if broadcastHandler != nil {
		mockServer.broadcastHandler = broadcastHandler
	} else {
		mockServer.broadcastHandler = mockServer.defaultBroadcastHandler
	}

	if txStatusHandler != nil {
		mockServer.txStatusHandler = txStatusHandler
	} else {
		mockServer.txStatusHandler = mockServer.defaultTxStatusHandler
	}

	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	sdktx.RegisterServiceServer(s, mockServer)
	tx.RegisterTxServer(s, mockServer)

	go func() {
		if err := s.Serve(lis); err != nil {
			t.Logf("Server exited with error: %v", err)
		}
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	mockTxClient, err := user.NewTxClient(
		encCfg.Codec,
		signer,
		conn,
		encCfg.InterfaceRegistry,
		opts...,
	)
	require.NoError(t, err)

	return mockTxClient, conn
}

func randomBlob(t *testing.T) *share.Blob {
	t.Helper()

	blob, err := share.NewBlob(share.RandomBlobNamespace(), []byte("hello world"), share.ShareVersionZero, nil)
	require.NoError(t, err)
	return blob
}

func hashTxBytes(txBytes []byte) string {
	sum := sha256.Sum256(txBytes)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func TestParallelSubmitPayForBlobSuccess(t *testing.T) {
	t.Parallel()

	statusStore := make(map[string]*tx.TxStatusResponse)
	var mu sync.Mutex

	broadcastHandler := func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
		hash := hashTxBytes(req.TxBytes)

		mu.Lock()
		statusStore[hash] = &tx.TxStatusResponse{
			Height:        33,
			ExecutionCode: abci.CodeTypeOK,
			Status:        core.TxStatusCommitted,
		}
		mu.Unlock()

		return &sdktx.BroadcastTxResponse{
			TxResponse: &sdktypes.TxResponse{
				Code:   abci.CodeTypeOK,
				TxHash: hash,
			},
		}, nil
	}
	txStatusHandler := func(ctx context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
		mu.Lock()
		defer mu.Unlock()

		if resp, ok := statusStore[strings.ToUpper(req.TxId)]; ok {
			return resp, nil
		}

		return &tx.TxStatusResponse{Status: core.TxStatusPending}, nil
	}

	workerAccounts := []string{"parallel-worker-1", "parallel-worker-2"}
	client, conn := newMockTxClientWithCustomHandlers(t, broadcastHandler, txStatusHandler, workerAccounts)
	defer conn.Close()
	require.NotNil(t, client.ParallelPool())

	// Start the parallel pool
	err := client.ParallelPool().Start(context.Background())
	require.NoError(t, err)

	blob := randomBlob(t)

	jobCount := 3
	results := make([]user.SubmissionResult, 0, jobCount)
	var resultChannels []chan user.SubmissionResult

	// Submit all jobs first
	for i := 0; i < jobCount; i++ {
		resultsC, err := client.SubmitPayForBlobParallel(context.Background(), []*share.Blob{blob})
		require.NoError(t, err)
		resultChannels = append(resultChannels, resultsC)
	}

	// Collect results from individual channels
	for i, resultsC := range resultChannels {
		select {
		case result := <-resultsC:
			require.NoError(t, result.Error)
			require.NotNil(t, result.TxResponse)
			require.Equal(t, abci.CodeTypeOK, result.TxResponse.Code)
			results = append(results, result)
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for result %d", i)
		}
	}

	var totalSequence uint64
	for _, worker := range client.ParallelPool().Workers() {
		totalSequence += client.Signer().Account(worker.AccountName()).Sequence()
	}
	require.Equal(t, uint64(len(results)), totalSequence)
}

func TestParallelSubmitPayForBlobBroadcastError(t *testing.T) {
	t.Parallel()

	broadcastHandler := func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
		hash := hashTxBytes(req.TxBytes)
		return &sdktx.BroadcastTxResponse{
			TxResponse: &sdktypes.TxResponse{
				Code:   5,
				TxHash: hash,
				RawLog: "insufficient funds",
			},
		}, nil
	}
	txStatusHandler := func(ctx context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
		return &tx.TxStatusResponse{Status: core.TxStatusCommitted}, nil
	}

	workerAccounts := []string{"parallel-worker-1"}
	client, conn := newMockTxClientWithCustomHandlers(t, broadcastHandler, txStatusHandler, workerAccounts)
	defer conn.Close()
	require.NotNil(t, client.ParallelPool())

	// Start the parallel pool
	err := client.ParallelPool().Start(context.Background())
	require.NoError(t, err)

	blob := randomBlob(t)

	resultsC, err := client.SubmitPayForBlobParallel(context.Background(), []*share.Blob{blob})
	require.NoError(t, err)

	// Get result from the submission-specific channel
	var result user.SubmissionResult
	select {
	case result = <-resultsC:
		require.Nil(t, result.TxResponse)
		require.Error(t, result.Error)
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for error result")
	}

	var broadcastErr *user.BroadcastTxError
	require.ErrorAs(t, result.Error, &broadcastErr)
	require.Equal(t, uint32(5), broadcastErr.Code)

	for _, worker := range client.ParallelPool().Workers() {
		require.Equal(t, uint64(0), client.Signer().Account(worker.AccountName()).Sequence())
	}
}

func TestParallelSubmissionSignerAddress(t *testing.T) {
	t.Parallel()

	statusStore := make(map[string]*tx.TxStatusResponse)
	var mu sync.Mutex

	broadcastHandler := func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
		hash := hashTxBytes(req.TxBytes)

		mu.Lock()
		statusStore[hash] = &tx.TxStatusResponse{
			Height:        33,
			ExecutionCode: abci.CodeTypeOK,
			Status:        core.TxStatusCommitted,
		}
		mu.Unlock()

		return &sdktx.BroadcastTxResponse{
			TxResponse: &sdktypes.TxResponse{
				Code:   abci.CodeTypeOK,
				TxHash: hash,
			},
		}, nil
	}
	txStatusHandler := func(ctx context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
		mu.Lock()
		defer mu.Unlock()

		if resp, ok := statusStore[strings.ToUpper(req.TxId)]; ok {
			return resp, nil
		}

		return &tx.TxStatusResponse{Status: core.TxStatusPending}, nil
	}

	workerAccounts := []string{"parallel-worker-1", "parallel-worker-2"}
	client, conn := newMockTxClientWithCustomHandlers(t, broadcastHandler, txStatusHandler, workerAccounts)
	defer conn.Close()
	require.NotNil(t, client.ParallelPool())

	// Start the parallel pool
	err := client.ParallelPool().Start(context.Background())
	require.NoError(t, err)

	blob := randomBlob(t)

	// Submit multiple jobs to test different workers
	jobCount := 4
	results := make([]user.SubmissionResult, 0, jobCount)
	var resultChannels []chan user.SubmissionResult

	// Submit all jobs first
	for i := 0; i < jobCount; i++ {
		resultsC, err := client.SubmitPayForBlobParallel(context.Background(), []*share.Blob{blob})
		require.NoError(t, err)
		resultChannels = append(resultChannels, resultsC)
	}

	// Collect results from individual channels
	for i, resultsC := range resultChannels {
		select {
		case result := <-resultsC:
			require.NoError(t, result.Error)
			require.NotNil(t, result.TxResponse)
			require.Equal(t, abci.CodeTypeOK, result.TxResponse.Code)

			// Verify that Signer field is populated with a valid address
			require.NotEmpty(t, result.Signer, "Signer address should not be empty")
			require.True(t, len(result.Signer) > 0, "Signer address should be non-empty")

			// Verify it looks like a valid address (basic validation)
			require.True(t, strings.HasPrefix(result.Signer, "celestia") || len(result.Signer) > 30,
				"Signer should look like a valid address, got: %s", result.Signer)

			results = append(results, result)
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for result %d", i)
		}
	}

	// Collect all unique signers
	signerSet := make(map[string]bool)
	for _, result := range results {
		signerSet[result.Signer] = true
	}

	// Verify we have at most as many unique signers as workers
	require.LessOrEqual(t, len(signerSet), len(workerAccounts),
		"Should not have more unique signers than workers")

	// Verify each signer corresponds to a worker address
	for signer := range signerSet {
		found := false
		for _, worker := range client.ParallelPool().Workers() {
			expectedAddr := client.Signer().Account(worker.AccountName()).Address().String()
			if signer == expectedAddr {
				found = true
				break
			}
		}
		require.True(t, found, "Signer %s should match a worker address", signer)
	}
}
