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

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	"github.com/celestiaorg/celestia-app/v8/test/util/random"
	"github.com/celestiaorg/celestia-app/v8/test/util/testnode"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/rpc/core"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
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
		// For mock tests, use WithTxWorkers since we're providing mock accounts
		option := user.WithTxWorkers(len(workerAccounts))
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

	// Start the tx queue (mirroring SetupTxClient behavior)
	err = mockTxClient.StartTxQueueForTest(context.Background())
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
	require.True(t, client.TxQueueWorkerCount() > 0)

	blob := randomBlob(t)

	jobCount := 3
	results := make([]user.SubmissionResult, 0, jobCount)
	var wg sync.WaitGroup
	var resultsMu sync.Mutex

	// Submit all jobs in parallel
	for range jobCount {
		wg.Go(func() {
			resp, err := client.SubmitPayForBlobToQueue(context.Background(), []*share.Blob{blob})
			resultsMu.Lock()
			defer resultsMu.Unlock()
			if err != nil {
				results = append(results, user.SubmissionResult{Error: err})
			} else {
				results = append(results, user.SubmissionResult{TxResponse: resp})
			}
		})
	}

	// Wait for all to complete
	wg.Wait()

	// Verify results
	for _, result := range results {
		require.NoError(t, result.Error)
		require.NotNil(t, result.TxResponse)
		require.Equal(t, abci.CodeTypeOK, result.TxResponse.Code)
	}

	var totalSequence uint64
	for i := 0; i < client.TxQueueWorkerCount(); i++ {
		accountName := client.TxQueueWorkerAccountName(i)
		totalSequence += client.Signer().Account(accountName).Sequence()
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
	require.True(t, client.TxQueueWorkerCount() > 0)

	blob := randomBlob(t)

	_, err := client.SubmitPayForBlobToQueue(context.Background(), []*share.Blob{blob})
	require.Error(t, err)

	var broadcastErr *user.BroadcastTxError
	require.ErrorAs(t, err, &broadcastErr)
	require.Equal(t, uint32(5), broadcastErr.Code)

	for i := 0; i < client.TxQueueWorkerCount(); i++ {
		accountName := client.TxQueueWorkerAccountName(i)
		require.Equal(t, uint64(0), client.Signer().Account(accountName).Sequence())
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
	require.True(t, client.TxQueueWorkerCount() > 0)

	blob := randomBlob(t)

	// Submit multiple jobs to test different workers
	jobCount := 4
	results := make([]user.SubmissionResult, 0, jobCount)
	var wg sync.WaitGroup
	var resultsMu sync.Mutex

	// Submit all jobs in parallel
	for range jobCount {
		wg.Go(func() {
			resp, err := client.SubmitPayForBlobToQueue(context.Background(), []*share.Blob{blob})

			resultsMu.Lock()
			defer resultsMu.Unlock()
			if err != nil {
				results = append(results, user.SubmissionResult{Error: err})
			} else {
				// Get the signer from the worker - for this test we need to track which worker was used
				// Since we can't easily get this from the response, we'll just verify the responses are valid
				results = append(results, user.SubmissionResult{TxResponse: resp})
			}
		})
	}

	// Wait for all to complete
	wg.Wait()

	// Verify all results
	for _, result := range results {
		require.NoError(t, result.Error)
		require.NotNil(t, result.TxResponse)
		require.Equal(t, abci.CodeTypeOK, result.TxResponse.Code)
	}

	// Verify that multiple workers were used by checking sequence numbers
	var totalSequence uint64
	for i := 0; i < client.TxQueueWorkerCount(); i++ {
		accountName := client.TxQueueWorkerAccountName(i)
		totalSequence += client.Signer().Account(accountName).Sequence()
	}
	require.Equal(t, uint64(len(results)), totalSequence)
}

func TestParallelPoolRestart(t *testing.T) {
	t.Parallel()

	statusStore := make(map[string]*tx.TxStatusResponse)
	var mu sync.Mutex

	broadcastHandler := func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
		hash := hashTxBytes(req.TxBytes)

		mu.Lock()
		statusStore[hash] = &tx.TxStatusResponse{
			Height:        12,
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

	workerAccounts := []string{"parallel-worker-1"}
	client, conn := newMockTxClientWithCustomHandlers(t, broadcastHandler, txStatusHandler, workerAccounts)
	defer conn.Close()

	require.True(t, client.TxQueueWorkerCount() > 0)

	ctx := context.Background()
	require.True(t, client.IsTxQueueStartedForTest())

	blob := randomBlob(t)

	submitAndAssert := func(t *testing.T) {
		t.Helper()

		resp, err := client.SubmitPayForBlobToQueue(ctx, []*share.Blob{blob})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, abci.CodeTypeOK, resp.Code)
	}

	submitAndAssert(t)

	client.StopTxQueueForTest()
	require.False(t, client.IsTxQueueStartedForTest())

	require.NoError(t, client.StartTxQueueForTest(ctx))
	require.True(t, client.IsTxQueueStartedForTest())

	submitAndAssert(t)
}

func TestParallelSubmitPayForBlobContextCancellation(t *testing.T) {
	t.Parallel()

	startedCh := make(chan struct{})
	broadcastHandler := func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
		close(startedCh)
		<-ctx.Done()
		return nil, ctx.Err()
	}

	txStatusHandler := func(ctx context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
		return &tx.TxStatusResponse{Status: core.TxStatusPending}, nil
	}

	workerAccounts := []string{"parallel-worker-1"}
	client, conn := newMockTxClientWithCustomHandlers(t, broadcastHandler, txStatusHandler, workerAccounts)
	defer conn.Close()

	require.True(t, client.TxQueueWorkerCount() > 0)

	blob := randomBlob(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := client.SubmitPayForBlobToQueue(ctx, []*share.Blob{blob})
		errCh <- err
	}()

	select {
	case <-startedCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("broadcast handler was not invoked")
	}

	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok, "expected gRPC status error")
		require.Equal(t, codes.Canceled, st.Code())
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for cancellation result")
	}
}

func TestSingleWorkerNoFeeGranter(t *testing.T) {
	t.Parallel()

	statusStore := make(map[string]*tx.TxStatusResponse)
	var mu sync.Mutex
	var capturedTx sdktypes.Tx

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	broadcastHandler := func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
		hash := hashTxBytes(req.TxBytes)

		// Decode and capture the transaction
		decodedTx, err := encCfg.TxConfig.TxDecoder()(req.TxBytes)
		if err != nil {
			return nil, err
		}
		capturedTx = decodedTx

		mu.Lock()
		statusStore[hash] = &tx.TxStatusResponse{
			Height:        100,
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

	// Create client with default single worker (no WithTxWorkers option)
	client, conn := newMockTxClientWithCustomHandlers(t, broadcastHandler, txStatusHandler, nil)
	defer conn.Close()
	require.Equal(t, 1, client.TxQueueWorkerCount())

	blob := randomBlob(t)

	// Submit via SubmitPayForBlobToQueue which uses the parallel pool
	resp, err := client.SubmitPayForBlobToQueue(context.Background(), []*share.Blob{blob})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, abci.CodeTypeOK, resp.Code)

	// Verify that no fee granter was set in the transaction
	feeTx, ok := capturedTx.(sdktypes.FeeTx)
	require.True(t, ok, "Transaction should implement FeeTx interface")
	require.Nil(t, feeTx.FeeGranter(), "Single worker should not use fee granter")
}

func TestParallelSubmissionV1BlobSignerOverride(t *testing.T) {
	t.Parallel()
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	numBlobs := 3
	txBytes := make([][]byte, 0, numBlobs)
	broadcastHandler := func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
		txBytes = append(txBytes, req.TxBytes)
		return &sdktx.BroadcastTxResponse{
			TxResponse: &sdktypes.TxResponse{Code: abci.CodeTypeOK, TxHash: hashTxBytes(req.TxBytes)},
		}, nil
	}

	txStatusHandler := func(ctx context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
		return &tx.TxStatusResponse{Status: core.TxStatusCommitted}, nil
	}

	workerAccounts := []string{"parallel-worker-1", "parallel-worker-2"}
	client, conn := newMockTxClientWithCustomHandlers(t, broadcastHandler, txStatusHandler, workerAccounts)
	defer conn.Close()

	for range numBlobs {
		blob, err := share.NewV1Blob(share.RandomBlobNamespace(), random.Bytes(100), testnode.RandomAddress().Bytes())
		require.NoError(t, err)

		resp, err := client.SubmitPayForBlobToQueue(context.Background(), []*share.Blob{blob})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)
	}

	require.Len(t, txBytes, numBlobs)

	for _, txBytes := range txBytes {
		blobTx, _, err := blobtx.UnmarshalBlobTx(txBytes)
		require.NoError(t, err)

		sdkTx, err := encCfg.TxConfig.TxDecoder()(blobTx.Tx)
		require.NoError(t, err)

		signers, _, err := encCfg.Codec.GetMsgV1Signers(sdkTx.GetMsgs()[0])
		require.NoError(t, err)
		txSigner := sdktypes.AccAddress(signers[0])

		for _, blob := range blobTx.Blobs {
			require.Equal(t, txSigner.Bytes(), blob.Signer())
		}
	}
}
