package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v12/app"
	"github.com/celestiaorg/celestia-app/v12/app/ante"
	"github.com/celestiaorg/celestia-app/v12/app/params"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v12/test/util"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// TestGovFailedReasonHasNoErrorText verifies that x/gov stores only the
// codespace and code of a failed proposal message in Proposal.FailedReason,
// not the error text. The text can hold Go-specific details, such as a
// pointer address printed with %d by gov Params.ValidateBasic.
func TestGovFailedReasonHasNoErrorText(t *testing.T) {
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName)
	negative := -time.Second

	testCases := []struct {
		name string
		msg  func(params govv1.Params) sdk.Msg
		want string
	}{
		{
			name: "gov params with negative max deposit period",
			msg: func(p govv1.Params) sdk.Msg {
				p.MaxDepositPeriod = &negative
				return &govv1.MsgUpdateParams{Authority: govAddr.String(), Params: p}
			},
			want: "proposal message failed: codespace undefined, code 1",
		},
		{
			name: "bank send without funds",
			msg: func(govv1.Params) sdk.Msg {
				return banktypes.NewMsgSend(govAddr, govAddr, sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(1))))
			},
			want: "proposal message failed: codespace sdk, code 5",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
			start := time.Now().UTC()
			ctx := testApp.NewUncachedContext(false, cmtproto.Header{Time: start})

			govParams, err := testApp.GovKeeper.Params.Get(ctx)
			require.NoError(t, err)
			proposal, err := testApp.GovKeeper.SubmitProposal(ctx, []sdk.Msg{tc.msg(govParams)}, "", "title", "summary", govAddr, false)
			require.NoError(t, err)
			require.NoError(t, testApp.GovKeeper.ActivateVotingPeriod(ctx, proposal))

			validators, err := testApp.StakingKeeper.GetAllValidators(ctx)
			require.NoError(t, err)
			for _, v := range validators {
				valAddr, err := sdk.ValAddressFromBech32(v.OperatorAddress)
				require.NoError(t, err)
				require.NoError(t, testApp.GovKeeper.AddVote(ctx, proposal.Id, sdk.AccAddress(valAddr), govv1.NewNonSplitVoteOption(govv1.OptionYes), ""))
			}

			ctx = ctx.WithBlockTime(start.Add(*govParams.VotingPeriod + time.Second))
			govModule, ok := testApp.ModuleManager.Modules[govtypes.ModuleName].(appmodule.HasEndBlocker)
			require.True(t, ok)
			require.NoError(t, govModule.EndBlock(ctx))

			got, err := testApp.GovKeeper.Proposals.Get(ctx, proposal.Id)
			require.NoError(t, err)
			require.Equal(t, govv1.StatusFailed, got.Status)
			require.Equal(t, tc.want, got.FailedReason)
		})
	}
}

func TestCortoQueuedProposalCannotRestoreOldParameters(t *testing.T) {
	a, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := a.NewUncachedContext(false, cmtproto.Header{Time: testutil.GenesisTime, ChainID: appconsts.CortoChainID})
	cp := app.DefaultConsensusParams()
	cp.Version.App = 11
	ctx = ctx.WithConsensusParams(*cp)
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName)
	staking, err := a.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	staking.UnbondingTime = appconsts.UnbondingTime
	require.NoError(t, a.StakingKeeper.SetParams(ctx, staking))
	old := &stakingtypes.MsgUpdateParams{Authority: govAddr.String(), Params: staking}
	require.NoError(t, ante.NewParamFilterDecorator(a.GovParamFilters()).ValidateMsgs(ctx, []sdk.Msg{old}))
	govParams, err := a.GovKeeper.Params.Get(ctx)
	require.NoError(t, err)
	changedGov := govParams
	changedGov.MinDepositRatio = "0.02"
	// This first message must roll back when the second message is rejected.
	proposal, err := a.GovKeeper.SubmitProposal(ctx, []sdk.Msg{&govv1.MsgUpdateParams{Authority: govAddr.String(), Params: changedGov}, old}, "", "title", "summary", govAddr, false)
	require.NoError(t, err)
	require.NoError(t, a.GovKeeper.ActivateVotingPeriod(ctx, proposal))
	vals, err := a.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	for _, val := range vals {
		addr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		require.NoError(t, a.GovKeeper.AddVote(ctx, proposal.Id, sdk.AccAddress(addr), govv1.NewNonSplitVoteOption(govv1.OptionYes), ""))
	}
	cp.Version.App = 12
	ctx = ctx.WithConsensusParams(*cp).WithBlockTime(testutil.GenesisTime.Add(*govParams.VotingPeriod + time.Second))
	staking.UnbondingTime = appconsts.CortoUnbondingTime
	require.NoError(t, a.StakingKeeper.SetParams(ctx, staking))
	require.NoError(t, a.ModuleManager.Modules[govtypes.ModuleName].(appmodule.HasEndBlocker).EndBlock(ctx))
	got, err := a.GovKeeper.Proposals.Get(ctx, proposal.Id)
	require.NoError(t, err)
	require.Equal(t, govv1.StatusFailed, got.Status)
	require.Equal(t, "proposal message failed: codespace sdk, code 4", got.FailedReason)
	after, err := a.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, staking, after)
	afterGov, err := a.GovKeeper.Params.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, govParams, afterGov)
}
