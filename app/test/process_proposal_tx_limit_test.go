package app_test

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestProcessProposalRejectsTxCountBeforeStateAccess(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), "account")
	limit := coretypes.MaxBlockTxs
	for _, tx := range [][]byte{nil, {1}} {
		req := &abci.RequestProcessProposal{Txs: make([][]byte, limit+1)}
		for i := range req.Txs {
			req.Txs[i] = tx
		}
		// Even a one-gas context must reject without reading the square-size
		// parameter, running BeginBlock or populating transaction caches.
		meter := storetypes.NewGasMeter(1)
		ctx := testApp.NewProposalContext(cmtproto.Header{Height: 1}).
			WithExecMode(sdk.ExecModeProcessProposal).WithGasMeter(meter)
		resp, err := testApp.ProcessProposalHandler(ctx, req)
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
		require.Zero(t, meter.GasConsumed(), "the count guard must precede all store access")
	}
}
