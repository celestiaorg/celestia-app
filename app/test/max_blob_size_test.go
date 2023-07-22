package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// TestMaxBlobSize verifies that a blob size of MaxBlobSize can not fit in a
// block.
func TestMaxBlobSize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestMaxBlobSize in short mode.")
	}
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := make([]string, 1)
	for i := range accounts {
		accounts[i] = tmrand.Str(20)
	}

	cparams := app.DefaultConsensusParams()
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(cparams, accounts...)
	ctx := sdk.NewContext(testApp.CommitMultiStore(), tmproto.Header{}, false, nil)

	maxBlobSize := testApp.MaxBlobSize(ctx)
	txs := testutil.RandBlobTxsWithAccounts(
		t,
		testApp,
		encConf.TxConfig.TxEncoder(),
		kr,
		maxBlobSize,
		1,
		false,
		testutil.ChainID,
		accounts,
	)
	resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
		BlockData: &core.Data{
			Txs: coretypes.Txs(txs).ToSliceOfBytes(),
		},
		ChainId: testutil.ChainID,
	})

	// Verify that the blob tx wasn't included in the block.
	require.Empty(t, 0, resp.BlockData.Txs)
}
