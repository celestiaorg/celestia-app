package test

import (
	"testing"

	tmlog "cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v4/app"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/x/paramfilter"
	"github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func TestParamFilter(t *testing.T) {
	app, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())

	require.Greater(t, len(app.BlockedParams()), 0)

	// check that all blocked parameters are in the filter keeper
	pph := paramfilter.NewParamBlockList(app.BlockedParams()...)
	for _, p := range app.BlockedParams() {
		require.True(t, pph.IsBlocked(p[0], p[1]))
	}

	handler := pph.GovHandler(app.ParamsKeeper)
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())

	for _, p := range app.BlockedParams() {
		p := testProposal(proposal.NewParamChange(p[0], p[1], "value"))
		err := handler(ctx, p)
		require.Error(t, err)
		require.Contains(t, err.Error(), "parameter can not be modified")
	}

	// ensure that we are not filtering out valid proposals
	validChange := proposal.NewParamChange(stakingtypes.ModuleName, string(stakingtypes.KeyMaxValidators), "1")
	p := testProposal(validChange)
	err := handler(ctx, p)
	require.NoError(t, err)

	ps := app.StakingKeeper.GetParams(ctx)
	require.Equal(t, ps.MaxValidators, uint32(1))

	// ensure that we're throwing out entire proposals if any of the changes are blocked
	for _, p := range app.BlockedParams() {
		// try to set the max validators to 2, unlike above this should fail
		validChange := proposal.NewParamChange(stakingtypes.ModuleName, string(stakingtypes.KeyMaxValidators), "2")
		invalidChange := proposal.NewParamChange(p[0], p[1], "value")
		p := testProposal(validChange, invalidChange)
		err := handler(ctx, p)
		require.Error(t, err)
		require.Contains(t, err.Error(), "parameter can not be modified")

		ps := app.StakingKeeper.GetParams(ctx)
		require.Equal(t, ps.MaxValidators, uint32(1))

	}
}

func testProposal(changes ...proposal.ParamChange) *proposal.ParameterChangeProposal {
	return proposal.NewParameterChangeProposal("title", "description", changes)
}
