package app

import (
	"testing"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func TestGovMsgHandler(t *testing.T) {
	failing := func(sdk.Context, sdk.Msg) (*sdk.Result, error) {
		return nil, errorsmod.Wrap(govtypes.ErrNoProposalHandlerExists, "detail [file.go:1]")
	}
	panicking := func(sdk.Context, sdk.Msg) (*sdk.Result, error) {
		panic("boom")
	}
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())
	endBlockCtx := ctx.WithValue(govEndBlockKey{}, true)

	require.Nil(t, govMsgHandler(nil))

	// Outside the EndBlocker the handler is unchanged.
	_, err := govMsgHandler(failing)(ctx, nil)
	require.Equal(t, "detail [file.go:1]: no handler exists for proposal type", err.Error())
	require.Panics(t, func() { _, _ = govMsgHandler(panicking)(ctx, nil) })

	_, err = govMsgHandler(failing)(endBlockCtx, nil)
	require.Equal(t, "proposal message failed: codespace gov, code 9", err.Error())
	require.ErrorIs(t, err, govtypes.ErrNoProposalHandlerExists)
	codespace, code, _ := errorsmod.ABCIInfo(err, false)
	require.Equal(t, govtypes.ModuleName, codespace)
	require.Equal(t, govtypes.ErrNoProposalHandlerExists.ABCICode(), code)

	_, err = govMsgHandler(panicking)(endBlockCtx, nil)
	require.Equal(t, "proposal message failed: codespace undefined, code 111222", err.Error())
	require.ErrorIs(t, err, sdkerrors.ErrPanic)
}

// The same chain/version policy applies at submission and at queued execution.
func TestCortoGovernancePolicy(t *testing.T) {
	for _, chain := range []string{appconsts.CortoChainID, appconsts.MochaChainID, appconsts.MainnetChainID, "other"} {
		for _, version := range []uint64{11, 12} {
			ctx := sdk.NewContext(nil, cmtproto.Header{ChainID: chain}, false, log.NewNopLogger()).WithConsensusParams(cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: version}})
			active := appconsts.CortoV12Active(chain, version)
			for _, corto := range []bool{false, true} {
				staking := stakingtypes.DefaultParams()
				staking.BondDenom = appconsts.BondDenom
				staking.UnbondingTime = appconsts.UnbondingTime
				evidence := EvidenceParams()
				if corto {
					staking.UnbondingTime = appconsts.CortoUnbondingTime
					evidence.MaxAgeDuration, evidence.MaxAgeNumBlocks = appconsts.CortoMaxAgeDuration, appconsts.CortoMaxAgeNumBlocks
				}
				for _, msg := range []sdk.Msg{
					&stakingtypes.MsgUpdateParams{Params: staking},
					&consensustypes.MsgUpdateParams{Evidence: evidence, Validator: DefaultConsensusParams().Validator, Block: DefaultConsensusParams().Block},
				} {
					proposal, err := govv1.NewMsgSubmitProposal([]sdk.Msg{msg}, nil, "", "", "title", "summary", false)
					require.NoError(t, err)
					nested := authz.NewMsgExec(sdk.AccAddress(make([]byte, 20)), []sdk.Msg{proposal})
					for _, wrapped := range []sdk.Msg{msg, proposal, &nested} {
						filter := ante.NewParamFilterDecorator((*App)(nil).GovParamFilters())
						err = filter.ValidateMsgs(ctx, []sdk.Msg{wrapped})
						require.Equal(t, corto != active, err != nil, "chain=%s version=%d corto=%t", chain, version, corto)
						called := false
						handler := govMsgHandler(func(sdk.Context, sdk.Msg) (*sdk.Result, error) { called = true; return &sdk.Result{}, nil })
						_, err = handler(ctx.WithValue(govEndBlockKey{}, true), wrapped)
						reject := active && !corto
						require.Equal(t, reject, err != nil)
						require.Equal(t, !reject, called)
						if reject {
							require.Equal(t, "proposal message failed: codespace sdk, code 4", err.Error())
						}
					}
				}
			}
		}
	}
}
