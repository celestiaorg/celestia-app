package test

import (
	"testing"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/paramfilter"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	"github.com/stretchr/testify/require"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestParamFilter(t *testing.T) {
	app, _ := testutil.SetupTestAppWithGenesisValSet()

	require.Greater(t, app.ForbiddenParams(), 0)

	// check that all forbidden parameters are in the filter keeper
	pfk := app.ParamFilterKeeper
	for _, p := range app.ForbiddenParams() {
		require.True(t, pfk.IsForbidden(p[0], p[1]))
	}

	handler := paramfilter.NewParamChangeProposalHandler(app.ParamFilterKeeper, app.ParamsKeeper)
	ctx := sdk.NewContext(app.CommitMultiStore(), types.Header{}, false, tmlog.NewNopLogger())

	for _, p := range app.ForbiddenParams() {
		proposal := testProposal(proposal.NewParamChange(p[0], p[1], "value"))
		err := handler(ctx, proposal)
		require.Error(t, err)
		require.Contains(t, err.Error(), "forbidden parameter change")
	}
}

func testProposal(changes ...proposal.ParamChange) *proposal.ParameterChangeProposal {
	return proposal.NewParameterChangeProposal("title", "description", changes)
}
