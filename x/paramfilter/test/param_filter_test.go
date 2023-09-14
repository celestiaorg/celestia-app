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
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	blockedParams, err := testApp.BlockedParams()
	require.NoError(t, err)
	require.Greater(t, len(blockedParams), 0)

	// check that all blocked parameters are in the filter keeper
	pph := paramfilter.NewParamBlockList(blockedParams...)
	for _, p := range blockedParams {
		require.True(t, pph.IsBlocked(p[0], p[1]))
	}

	handler := pph.GovHandler(testApp.ParamsKeeper)
	ctx := sdk.NewContext(testApp.CommitMultiStore(), tmproto.Header{}, false, tmlog.NewNopLogger())

	t.Run("test that a proposal with a blocked param is rejected", func(t *testing.T) {
		for _, p := range blockedParams {
			p := testProposal(proposal.NewParamChange(p[0], p[1], "value"))
			err := handler(ctx, p)
			require.Error(t, err)
			require.ErrorIs(t, err, paramfilter.ErrBlockedParameter)
		}
	})

	t.Run("test that a proposal with an unblocked params is accepted", func(t *testing.T) {
		// Ensure that MaxValidators has not been modified
		require.Equal(t, stakingtypes.DefaultMaxValidators, testApp.StakingKeeper.GetParams(ctx).MaxValidators)

		newMaxValidators := uint32(1)
		validChange := proposal.NewParamChange(stakingtypes.ModuleName, string(stakingtypes.KeyMaxValidators), fmt.Sprint(newMaxValidators))

		p := testProposal(validChange)
		err := handler(ctx, p)
		require.NoError(t, err)
		require.Equal(t, newMaxValidators, testApp.StakingKeeper.GetParams(ctx).MaxValidators)
	})

	t.Run("test that a proposal with a blocked param and an unblocked param is rejected", func(t *testing.T) {
		for _, p := range blockedParams {
			ps := testApp.StakingKeeper.GetParams(ctx)
			// Ensure that MaxEntries has not been modified
			require.Equal(t, stakingtypes.DefaultMaxEntries, ps.MaxEntries)

			newMaxEntries := 8
			validChange := proposal.NewParamChange(stakingtypes.ModuleName, string(stakingtypes.KeyMaxEntries), fmt.Sprint(newMaxEntries))
			invalidChange := proposal.NewParamChange(p[0], p[1], "value")

			p := testProposal(validChange, invalidChange)
			err := handler(ctx, p)
			require.Error(t, err)
			require.ErrorIs(t, err, paramfilter.ErrBlockedParameter)
			// Ensure that MaxEntries has not been modified
			require.Equal(t, stakingtypes.DefaultMaxEntries, ps.MaxEntries)
		}
	})

	t.Run("test evidence params with different app versions", func(t *testing.T) {
		t.Run("evidence params are unblocked in v1", func(t *testing.T) {
			defaults := coretypes.DefaultEvidenceParams()
			// Ensure that the evidence params haven't been modified yet
			require.Equal(t, defaults, *testApp.GetConsensusParams(ctx).Evidence)

			updated := tmproto.EvidenceParams{
				MaxAgeNumBlocks: defaults.MaxAgeNumBlocks + 1,
				MaxAgeDuration:  1,
				MaxBytes:        defaults.MaxBytes,
			}
			require.NoError(t, baseapp.ValidateEvidenceParams(updated))

			marshalled, err := testApp.AppCodec().MarshalJSON(&defaults)
			require.NoError(t, err)

			validChange := proposal.NewParamChange(baseapp.Paramspace, string(baseapp.ParamStoreKeyEvidenceParams), string(marshalled))
			p := testProposal(validChange)
			err = handler(ctx, p)
			require.NoError(t, err) // TODO this should not be an error

			// Ensure that the evidence params have been modified
			require.Equal(t, updated, *testApp.GetConsensusParams(ctx).Evidence)
		})

		t.Run("evidence params are blocked in v2", func(t *testing.T) {
			defaults := coretypes.DefaultEvidenceParams()
			// Ensure that the evidence params haven't been modified yet
			require.Equal(t, defaults, *testApp.GetConsensusParams(ctx).Evidence)

			updated := tmproto.EvidenceParams{
				MaxAgeNumBlocks: defaults.MaxAgeNumBlocks + 1,
				MaxAgeDuration:  1,
				MaxBytes:        defaults.MaxBytes,
			}
			require.NoError(t, baseapp.ValidateEvidenceParams(updated))

			marshalled, err := testApp.AppCodec().MarshalJSON(&defaults)
			require.NoError(t, err)

			invalidChange := proposal.NewParamChange(baseapp.Paramspace, string(baseapp.ParamStoreKeyEvidenceParams), string(marshalled))
			p := testProposal(invalidChange)
			err = handler(ctx, p)
			require.Error(t, err)
			require.ErrorIs(t, err, paramfilter.ErrBlockedParameter)

			// Ensure that the evidence params still haven't been modified
			require.Equal(t, defaults, *testApp.GetConsensusParams(ctx).Evidence)
		})
	})
}

func testProposal(changes ...proposal.ParamChange) *proposal.ParameterChangeProposal {
	return proposal.NewParameterChangeProposal("title", "description", changes)
}
