package app_test

import (
	"testing"

	"cosmossdk.io/collections"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
)

func TestV11UpgradeCancellationRatio(t *testing.T) {
	testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithBlockHeight(1)
	before, err := testApp.GovKeeper.Params.Get(ctx)
	require.NoError(t, err)
	before.ProposalCancelRatio = "0.500000000000000000"
	before.ProposalCancelDest = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	before.BurnVoteQuorum = true
	require.NoError(t, testApp.GovKeeper.Params.Set(ctx, before))

	require.True(t, testApp.UpgradeKeeper.HasHandler("v11"))
	require.NoError(t, testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "v11", Height: 1}))
	after, err := testApp.GovKeeper.Params.Get(ctx)
	require.NoError(t, err)
	before.ProposalCancelRatio = "0.050000000000000000"
	require.Equal(t, before, after)
}

func TestV11UpgradeFailsWithoutGovParams(t *testing.T) {
	testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithBlockHeight(1)
	require.NoError(t, testApp.GovKeeper.Params.Remove(ctx))
	require.ErrorIs(t, testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "v11", Height: 1}), collections.ErrNotFound)
}

func TestGovCancellationRatioRestriction(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version uint64
		ratio   string
		blocked bool
	}{
		{"v10 retains governance control", 10, "0.10", false},
		{"v11 rejects old ratio", 11, "0.50", true},
		{"v11 rejects free cancellation", 11, "0", true},
		{"v11 accepts fixed ratio", 11, "0.050000000000000000", false},
		{"v11 accepts equivalent decimal", 11, "0.05", false},
		{"later versions retain restriction", 12, "0.10", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
			consensusParams := *app.DefaultConsensusParams()
			consensusParams.Version.App = tc.version
			ctx := testApp.NewContext(false).WithConsensusParams(consensusParams)
			before, err := testApp.GovKeeper.Params.Get(ctx)
			require.NoError(t, err)
			before.ProposalCancelRatio = "0.050000000000000000"
			require.NoError(t, testApp.GovKeeper.Params.Set(ctx, before))
			updated := before
			updated.ProposalCancelRatio = tc.ratio
			updated.BurnVoteQuorum = !before.BurnVoteQuorum
			msg := &govv1.MsgUpdateParams{Authority: testApp.GovKeeper.GetAuthority(), Params: updated}
			handler := testApp.MsgServiceRouter().Handler(msg)
			require.NotNil(t, handler)
			_, err = handler(ctx, msg)
			if tc.blocked {
				require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
			} else {
				require.NoError(t, err)
			}
			after, err := testApp.GovKeeper.Params.Get(ctx)
			require.NoError(t, err)
			if tc.blocked {
				require.Equal(t, before, after)
			} else {
				require.Equal(t, updated, after)
			}
		})
	}
}

func TestGovCancellationRatioQueuedBeforeV11(t *testing.T) {
	proposer := testfactory.GenerateAccounts(1)[0]
	testApp, keyring := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), proposer)
	consensusParams := *app.DefaultConsensusParams()
	consensusParams.Version.App = 10
	ctx := testApp.NewContext(false).WithConsensusParams(consensusParams).WithBlockHeight(1)
	params, err := testApp.GovKeeper.Params.Get(ctx)
	require.NoError(t, err)
	params.ProposalCancelRatio = "0.500000000000000000"
	require.NoError(t, testApp.GovKeeper.Params.Set(ctx, params))
	msg := &govv1.MsgUpdateParams{Authority: testApp.GovKeeper.GetAuthority(), Params: params}
	proposal, err := testApp.GovKeeper.SubmitProposal(ctx, []sdk.Msg{msg}, "", "Restore old params", "Restore the previous cancellation ratio", testfactory.GetAddress(keyring, proposer), false)
	require.NoError(t, err)

	require.NoError(t, testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "v11", Height: 1}))
	consensusParams.Version.App = 11
	ctx = ctx.WithConsensusParams(consensusParams)
	messages, err := proposal.GetMsgs()
	require.NoError(t, err)
	// Governance dispatches queued proposal messages through this same router.
	_, err = testApp.MsgServiceRouter().Handler(messages[0])(ctx, messages[0])
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	got, err := testApp.GovKeeper.Params.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, "0.050000000000000000", got.ProposalCancelRatio)
}

func TestGovCancellationRatioSubmission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version uint64
		ratio   string
		blocked bool
	}{
		{"v10 accepts ratio change", 10, "0.10", false},
		{"v11 rejects ratio change", 11, "0.10", true},
		{"v11 accepts other changes", 11, "0.05", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proposer := testfactory.GenerateAccounts(1)[0]
			testApp, keyring := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), proposer)
			consensusParams := *app.DefaultConsensusParams()
			consensusParams.Version.App = tc.version
			ctx := testApp.NewContext(false).WithConsensusParams(consensusParams)
			params, err := testApp.GovKeeper.Params.Get(ctx)
			require.NoError(t, err)
			deposit := sdk.NewCoins(params.MinDeposit...)
			params.ProposalCancelRatio = tc.ratio
			params.BurnVoteQuorum = !params.BurnVoteQuorum
			update := &govv1.MsgUpdateParams{Authority: testApp.GovKeeper.GetAuthority(), Params: params}
			msg, err := govv1.NewMsgSubmitProposal([]sdk.Msg{update}, deposit, testfactory.GetAddress(keyring, proposer).String(), "", "Update governance params", "Change governance settings", false)
			require.NoError(t, err)
			_, err = testApp.MsgServiceRouter().Handler(msg)(ctx, msg)
			if tc.blocked {
				require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
