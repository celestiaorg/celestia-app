package utils

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	"github.com/celestiaorg/celestia-app/v8/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v8/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v8/x/blob/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// indexerWaitTimeout bounds how long GetTxWithRetry waits for the tx indexer
// to index a committed tx before giving up.
const indexerWaitTimeout = 10 * time.Second

func SetupTxClient(
	t *testing.T,
	ttlNumBlocks int64,
	squareSize uint64,
	blocksize int64,
	opts ...user.Option,
) (encoding.Config, *user.TxClient, testnode.Context) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.Mempool.TTLNumBlocks = ttlNumBlocks
	accounts := testfactory.GenerateAccounts(3)

	blobParams := blobtypes.DefaultParams()
	blobParams.GovMaxSquareSize = squareSize

	testnodeConfig := testnode.DefaultConfig().
		WithTendermintConfig(tmConfig).
		WithFundedAccounts(accounts...).
		WithDelayedPrecommitTimeout(300 * time.Millisecond).
		WithModifiers(genesis.SetBlobParams(enc.Codec, blobParams)).
		WithMaxBytes(blocksize)

	ctx, _, _ := testnode.NewNetwork(t, testnodeConfig)
	_, err := ctx.WaitForHeight(1)
	require.NoError(t, err)

	txClient, err := user.SetupTxClient(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, enc, opts...)
	require.NoError(t, err)

	return enc, txClient, ctx
}

func SetupTxClientWithDefaultParams(t *testing.T, opts ...user.Option) (encoding.Config, *user.TxClient, testnode.Context) {
	return SetupTxClient(t, 0, 128, 8388608, opts...) // no ttl and 8MiB block size
}

// GetTxWithRetry polls ServiceClient.GetTx until the tx is indexed or
// indexerWaitTimeout elapses. The CometBFT TxStatus RPC (used by
// TxClient.ConfirmTx) and the Cosmos SDK tx indexer (used by
// ServiceClient.GetTx) are updated asynchronously after a block is
// committed, so GetTx can return NotFound for a tx that ConfirmTx has just
// reported as committed. Tests should prefer this helper whenever they need
// to look up a tx by hash immediately after submitting it.
func GetTxWithRetry(ctx context.Context, serviceClient sdktx.ServiceClient, txHash string) (*sdktx.GetTxResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, indexerWaitTimeout)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		resp, err := serviceClient.GetTx(ctx, &sdktx.GetTxRequest{Hash: txHash})
		if err == nil {
			return resp, nil
		}
		if status.Code(err) != codes.NotFound {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for tx %s to be indexed: %w", txHash, ctx.Err())
		case <-ticker.C:
		}
	}
}

func VerifyTxResponse(
	t *testing.T,
	ctx context.Context,
	serviceClient sdktx.ServiceClient,
	confirmTxResp any,
) {
	var (
		expTxHash    string
		expCode      uint32
		expCodespace string
		expGasWanted int64
		expGasUsed   int64
		expHeight    int64
	)

	switch v := confirmTxResp.(type) {
	case *user.TxResponse:
		expTxHash, expCode, expCodespace, expGasWanted, expGasUsed, expHeight = v.TxHash, v.Code, v.Codespace, v.GasWanted, v.GasUsed, v.Height
	case *sdktypes.TxResponse:
		expTxHash, expCode, expCodespace, expGasWanted, expGasUsed, expHeight = v.TxHash, v.Code, v.Codespace, v.GasWanted, v.GasUsed, v.Height
	default:
		require.FailNowf(t, "unexpected type", "unsupported confirmTxResp type: %T", confirmTxResp)
	}

	getTxResp, err := GetTxWithRetry(ctx, serviceClient, expTxHash)
	require.NoError(t, err)

	txResp := getTxResp.TxResponse
	require.NotNil(t, txResp)
	require.Empty(t, txResp.RawLog)
	require.Equal(t, expCode, txResp.Code)
	require.Equal(t, expCodespace, txResp.Codespace)
	require.Equal(t, expGasWanted, txResp.GasWanted)
	require.Equal(t, expGasUsed, txResp.GasUsed)
	require.Equal(t, expHeight, txResp.Height)
}
