package test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/paramfilter"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	tmlog "github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
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
	ctx := sdk.NewContext(app.CommitMultiStore(), tmproto.Header{}, false, tmlog.NewNopLogger())

	t.Run("test that a proposal with a blocked param is rejected", func(t *testing.T) {
		for _, p := range app.BlockedParams() {
			p := testProposal(proposal.NewParamChange(p[0], p[1], "value"))
			err := handler(ctx, p)
			require.Error(t, err)
			require.Contains(t, err.Error(), "parameter can not be modified")
		}
	})

	t.Run("test that a proposal with an unblocked params is accepted", func(t *testing.T) {
		ps := app.StakingKeeper.GetParams(ctx)
		// Ensure that MaxValidators has not been modified
		require.Equal(t, stakingtypes.DefaultMaxValidators, ps.MaxValidators)

		newMaxValidators := 1
		validChange := proposal.NewParamChange(stakingtypes.ModuleName, string(stakingtypes.KeyMaxValidators), fmt.Sprint(newMaxValidators))

		p := testProposal(validChange)
		err := handler(ctx, p)
		require.NoError(t, err)
		require.Equal(t, newMaxValidators, ps.MaxValidators)
	})

	t.Run("test that a proposal with a blocked param and an unblocked param is rejected", func(t *testing.T) {
		for _, p := range app.BlockedParams() {
			ps := app.StakingKeeper.GetParams(ctx)
			// Ensure that MaxEntries has not been modified
			require.Equal(t, stakingtypes.DefaultMaxEntries, ps.MaxEntries)

			newMaxEntries := 8
			validChange := proposal.NewParamChange(stakingtypes.ModuleName, string(stakingtypes.KeyMaxEntries), fmt.Sprint(newMaxEntries))
			invalidChange := proposal.NewParamChange(p[0], p[1], "value")

			p := testProposal(validChange, invalidChange)
			err := handler(ctx, p)
			require.Error(t, err)
			require.Contains(t, err.Error(), "parameter can not be modified")
			require.Equal(t, stakingtypes.DefaultMaxEntries, ps.MaxEntries)
		}
	})

	t.Run("test if evidence params can be updated", func(t *testing.T) {
		defaults := coretypes.DefaultEvidenceParams()

		// Ensure that the evidence params haven't been modified yet
		require.Equal(t, defaults, *app.GetConsensusParams(ctx).Evidence)

		updated := tmproto.EvidenceParams{
			MaxAgeNumBlocks: defaults.MaxAgeNumBlocks + 1,
			MaxAgeDuration:  defaults.MaxAgeDuration,
			MaxBytes:        defaults.MaxBytes,
		}
		require.NoError(t, baseapp.ValidateEvidenceParams(updated))
		validChange := proposal.NewParamChange(baseapp.Paramspace, string(baseapp.ParamStoreKeyEvidenceParams), updated.String())

		p := testProposal(validChange)
		err := handler(ctx, p)

		require.NoError(t, err) // fails with: key: EvidenceParams, value: max_age_num_blocks:100001 max_age_duration:<seconds:172800 > max_bytes:1048576 , err: invalid character 'm' looking for beginning of value: failed to set parameter
		require.Equal(t, updated, app.GetConsensusParams(ctx).Evidence)
	})
}

func testProposal(changes ...proposal.ParamChange) *proposal.ParameterChangeProposal {
	return proposal.NewParameterChangeProposal("title", "description", changes)
}
