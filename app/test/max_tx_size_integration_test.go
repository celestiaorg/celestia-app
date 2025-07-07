package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v5/app"
	"github.com/celestiaorg/celestia-app/v5/app/encoding"
	apperrors "github.com/celestiaorg/celestia-app/v5/app/errors"
	"github.com/celestiaorg/celestia-app/v5/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v5/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v5/test/util/random"
	"github.com/celestiaorg/celestia-app/v5/test/util/testnode"
	"github.com/stretchr/testify/require"
)

// TestTxsOverMaxTxSizeGetRejected tests that transactions over the max tx size get rejected
// by the application even if the validator node's local mempool config allows them.
func TestTxsOverMaxTxSizeGetRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping max tx size integration test in short mode.")
	}

	accounts := testnode.RandomAccounts(10)
	// Set the max block size to 8MB
	cparams := testnode.DefaultConsensusParams()
	cparams.Block.MaxBytes = 8_388_608 // 8MiB
	cfg := testnode.DefaultConfig().WithFundedAccounts(accounts...).WithConsensusParams(cparams)

	// Set the max tx bytes to 3MiB in the node's mempool
	mempoolMaxTxBytes := appconsts.MaxTxSize + 1_048_576 // 2MiB + 1MiB
	cfg.TmConfig.Mempool.MaxTxBytes = mempoolMaxTxBytes

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	require.NoError(t, cctx.WaitForNextBlock())

	// Create 10 blob txs that exceed the max tx bytes
	txs := blobfactory.RandBlobTxsWithAccounts(
		ecfg,
		random.New(),
		cctx.Keyring,
		cctx.GRPCClient,
		appconsts.MaxTxSize,
		1,
		false,
		accounts,
	)

	hashes := make([]string, len(txs))
	for i, tx := range txs {
		res, err := cctx.BroadcastTxSync(tx)
		require.NoError(t, err)
		require.Equal(t, apperrors.ErrTxExceedsMaxSize.ABCICode(), res.Code)
		require.Contains(t, res.RawLog, apperrors.ErrTxExceedsMaxSize.Error())
		hashes[i] = res.TxHash
	}

	// Wait for a few blocks to ensure the tx is rejected during recheck
	require.NoError(t, cctx.WaitForBlocks(3))

	// Verify the transaction was not included in any block
	for _, hash := range hashes {
		txResp, err := testnode.QueryTx(cctx.Context, hash, true)
		require.Error(t, err)
		require.Nil(t, txResp)
	}
}
