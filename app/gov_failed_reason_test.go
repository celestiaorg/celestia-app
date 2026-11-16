package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/params"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
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
