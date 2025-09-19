package user_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/util/grpctest"
	"github.com/celestiaorg/go-square/v3/share"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/rpc/core"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

func newMockTxClient(t *testing.T, svc *grpctest.MockTxService, workerAccounts []string) *user.TxClient {
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

	conn := grpctest.StartBufConnMockServer(t, svc)

	options := []user.Option{
		user.WithPollTime(10 * time.Millisecond),
	}
	if len(workerAccounts) > 0 {
		options = append(options, user.WithTxWorkers(len(workerAccounts), workerAccounts))
	}

	client, err := user.NewTxClient(encCfg.Codec, signer, conn, encCfg.InterfaceRegistry, options...)
	require.NoError(t, err)

	return client
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

	svc := &grpctest.MockTxService{}
	svc.BroadcastHandler = func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
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
	svc.TxStatusHandler = func(ctx context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
		mu.Lock()
		defer mu.Unlock()

		if resp, ok := statusStore[strings.ToUpper(req.TxId)]; ok {
			return resp, nil
		}

		return &tx.TxStatusResponse{Status: core.TxStatusPending}, nil
	}

	workerAccounts := []string{"worker-1", "worker-2"}
	client := newMockTxClient(t, svc, workerAccounts)
	require.NotNil(t, client.ParallelPool())

	blob := randomBlob(t)

	jobCount := 3
	results := make([]*user.SubmissionResult, 0, jobCount)

	for i := 0; i < jobCount; i++ {
		resCh, err := client.SubmitPayForBlobParallel(context.Background(), []*share.Blob{blob})
		require.NoError(t, err)
		result := <-resCh
		require.NotNil(t, result)
		require.NoError(t, result.Error)
		require.NotNil(t, result.TxResponse)
		require.Equal(t, abci.CodeTypeOK, result.TxResponse.Code)
		results = append(results, result)
	}

	var totalSequence uint64
	for _, worker := range client.ParallelPool().Workers() {
		totalSequence += client.Signer().Account(worker.AccountName()).Sequence()
	}
	require.Equal(t, uint64(len(results)), totalSequence)
}

func TestParallelSubmitPayForBlobBroadcastError(t *testing.T) {
	t.Parallel()

	svc := &grpctest.MockTxService{}
	svc.BroadcastHandler = func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
		hash := hashTxBytes(req.TxBytes)
		return &sdktx.BroadcastTxResponse{
			TxResponse: &sdktypes.TxResponse{
				Code:   5,
				TxHash: hash,
				RawLog: "insufficient funds",
			},
		}, nil
	}
	svc.TxStatusHandler = func(ctx context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
		return &tx.TxStatusResponse{Status: core.TxStatusCommitted}, nil
	}

	workerAccounts := []string{"worker-1"}
	client := newMockTxClient(t, svc, workerAccounts)

	blob := randomBlob(t)

	resCh, err := client.SubmitPayForBlobParallel(context.Background(), []*share.Blob{blob})
	require.NoError(t, err)
	result := <-resCh
	require.NotNil(t, result)
	require.Nil(t, result.TxResponse)
	require.Error(t, result.Error)

	var broadcastErr *user.BroadcastTxError
	require.ErrorAs(t, result.Error, &broadcastErr)
	require.Equal(t, uint32(5), broadcastErr.Code)

	for _, worker := range client.ParallelPool().Workers() {
		require.Equal(t, uint64(0), client.Signer().Account(worker.AccountName()).Sequence())
	}
}
