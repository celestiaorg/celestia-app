package app_test

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/app"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestProcessProposalRejectsTxSafetyLimitBeforeStateAccess(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), "account")
	req := &abci.RequestProcessProposal{Txs: make([][]byte, coretypes.MaxBlockTxsV10+1)}
	meter := storetypes.NewGasMeter(1)
	ctx := testApp.NewProposalContext(cmtproto.Header{Height: 1}).
		WithExecMode(sdk.ExecModeProcessProposal).WithGasMeter(meter)

	resp, err := testApp.ProcessProposalHandler(ctx, req)
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
	require.Zero(t, meter.GasConsumed(), "the safety guard must precede all store access")
}

func TestTxSafetyLimitExceedsV10BlockCapacity(t *testing.T) {
	// Every transaction accepted by v10 ProcessProposal contains a valid
	// 64-byte signature. Even without protobuf field overhead, Core's hard
	// block-size maximum cannot contain enough accepted transactions to reach
	// the safety ceiling.
	require.Greater(t, coretypes.MaxBlockTxsV10*64, coretypes.DefaultMaxBlockSizeBytes)
}
