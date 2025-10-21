package utils

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

func SetupTxClient(
	t *testing.T,
	ttlNumBlocks int64,
	blocksize int64,
	opts ...user.Option,
) (encoding.Config, *user.TxClient, testnode.Context) {
	defaultTmConfig := testnode.DefaultTendermintConfig()
	defaultTmConfig.Mempool.TTLNumBlocks = ttlNumBlocks
	accounts := testfactory.GenerateAccounts(3)

	testnodeConfig := testnode.DefaultConfig().
		WithTendermintConfig(defaultTmConfig).
		WithFundedAccounts(accounts...).
		WithDelayedPrecommitTimeout(300 * time.Millisecond)
	testnodeConfig.Genesis.ConsensusParams.Block.MaxBytes = blocksize

	ctx, _, _ := testnode.NewNetwork(t, testnodeConfig)
	_, err := ctx.WaitForHeight(1)
	require.NoError(t, err)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	txClient, err := user.SetupTxClient(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, enc, opts...)
	require.NoError(t, err)

	return enc, txClient, ctx
}

func SetupTxClientWithDefaultParams(t *testing.T, opts ...user.Option) (encoding.Config, *user.TxClient, testnode.Context) {
	return SetupTxClient(t, 0, 8388608, opts...) // no ttl and 8MiB block size
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
		expTxHash, expCode, expCodespace, expGasWanted, expGasUsed, expHeight =
			v.TxHash, v.Code, v.Codespace, v.GasWanted, v.GasUsed, v.Height
	case *sdktypes.TxResponse:
		expTxHash, expCode, expCodespace, expGasWanted, expGasUsed, expHeight =
			v.TxHash, v.Code, v.Codespace, v.GasWanted, v.GasUsed, v.Height
	default:
		require.FailNowf(t, "unexpected type", "unsupported confirmTxResp type: %T", confirmTxResp)
	}

	getTxResp, err := serviceClient.GetTx(ctx, &sdktx.GetTxRequest{Hash: expTxHash})
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
