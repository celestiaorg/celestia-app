package ante_test

import (
	"testing"

	evidencetypes "cosmossdk.io/x/evidence/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

func TestUnsupportedMsgDecorator(t *testing.T) {
	unsupported := []sdk.Msg{
		&evidencetypes.MsgSubmitEvidence{},
		&govv1.MsgExecLegacyContent{},
		&govv1beta1.MsgSubmitProposal{},
		&vestingtypes.MsgCreatePeriodicVestingAccount{},
		&vestingtypes.MsgCreatePermanentLockedAccount{},
		&channeltypes.MsgChannelCloseInit{},
		&channeltypes.MsgChannelUpgradeInit{},
	}

	type testCase struct {
		name    string
		msg     sdk.Msg
		wantErr error
	}
	tests := make([]testCase, 0, 5+3*len(unsupported))
	tests = append(tests,
		testCase{name: "Accept MsgSend", msg: &banktypes.MsgSend{}},
		testCase{name: "Accept MsgCreateVestingAccount", msg: &vestingtypes.MsgCreateVestingAccount{}},
		testCase{name: "Accept MsgExec with MsgSend", msg: newMsgExec(&banktypes.MsgSend{})},
		testCase{name: "Accept MsgSubmitProposal with MsgSend", msg: newMsgSubmitProposal(t, &banktypes.MsgSend{})},
		testCase{name: "Accept MsgChannelCloseConfirm", msg: &channeltypes.MsgChannelCloseConfirm{}},
	)
	for _, msg := range unsupported {
		name := sdk.MsgTypeURL(msg)
		tests = append(tests,
			testCase{name: "Reject " + name, msg: msg, wantErr: sdkerrors.ErrNotSupported},
			testCase{name: "Reject " + name + " inside MsgExec", msg: newMsgExec(msg), wantErr: sdkerrors.ErrNotSupported},
			testCase{name: "Reject " + name + " inside MsgSubmitProposal", msg: newMsgSubmitProposal(t, msg), wantErr: sdkerrors.ErrNotSupported},
		)
	}

	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	anteHandler := sdk.ChainAnteDecorators(ante.NewUnsupportedMsgDecorator())

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.NewContext(nil, cmtproto.Header{}, true, nil)
			txBuilder := cdc.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.msg))

			_, err := anteHandler(ctx, txBuilder.GetTx(), false)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func newMsgExec(msgs ...sdk.Msg) *authz.MsgExec {
	msgExec := authz.NewMsgExec(sdk.AccAddress{}, msgs)
	return &msgExec
}

// TestAppAnteHandlerRejectsUnsupportedMsg checks that the app's ante handler
// chain includes UnsupportedMsgDecorator.
func TestAppAnteHandlerRejectsUnsupportedMsg(t *testing.T) {
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	txBuilder := testApp.GetEncodingConfig().TxConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(&evidencetypes.MsgSubmitEvidence{}))
	txBuilder.SetGasLimit(1_000_000)

	ctx := testApp.NewUncachedContext(false, cmtproto.Header{})
	_, err := testApp.AnteHandler()(ctx, txBuilder.GetTx(), false)
	require.ErrorIs(t, err, sdkerrors.ErrNotSupported)
}
