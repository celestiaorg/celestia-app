package test

import (
	"testing"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/paramfilter"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestParamFilter(t *testing.T) {
	app, _ := testutil.SetupTestAppWithGenesisValSet()

	require.Greater(t, len(app.ForbiddenParams()), 0)

	// check that all forbidden parameters are in the filter keeper
	pfk := app.ParamFilterKeeper
	for _, p := range app.ForbiddenParams() {
		require.True(t, pfk.IsForbidden(p[0], p[1]))
	}

	handler := paramfilter.NewParamChangeProposalHandler(app.ParamFilterKeeper, app.ParamsKeeper)
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())

	for _, p := range app.ForbiddenParams() {
		p := testProposal(proposal.NewParamChange(p[0], p[1], "value"))
		err := handler(ctx, p)
		require.Error(t, err)
		require.Contains(t, err.Error(), "forbidden parameter change")
	}

	// ensure that we are not filtering out valid proposals
	validChange := proposal.NewParamChange(stakingtypes.ModuleName, string(stakingtypes.KeyMaxValidators), "1")
	p := testProposal(validChange)
	err := handler(ctx, p)
	require.NoError(t, err)

	ps := app.StakingKeeper.GetParams(ctx)
	require.Equal(t, ps.MaxValidators, uint32(1))

	// ensure that we're throwing out entire proposals if any of the changes are forbidden
	for _, p := range app.ForbiddenParams() {
		// try to set the max validators to 2, unlike above this should fail
		validChange := proposal.NewParamChange(stakingtypes.ModuleName, string(stakingtypes.KeyMaxValidators), "2")
		invalidChange := proposal.NewParamChange(p[0], p[1], "value")
		p := testProposal(validChange, invalidChange)
		err := handler(ctx, p)
		require.Error(t, err)
		require.Contains(t, err.Error(), "forbidden parameter change")

		ps := app.StakingKeeper.GetParams(ctx)
		require.Equal(t, ps.MaxValidators, uint32(1))

	}
}

func testProposal(changes ...proposal.ParamChange) *proposal.ParameterChangeProposal {
	return proposal.NewParameterChangeProposal("title", "description", changes)
}
