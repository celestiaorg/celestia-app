package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/ante"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
)

func TestNestedMsgDecorator(t *testing.T) {
	msgExec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&banktypes.MsgSend{}})
	nestedMsgExec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&msgExec})
	nestedMsgPayForBlobs := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&blobtypes.MsgPayForBlobs{}})
	nestedMsgPayForFibre := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&fibretypes.MsgPayForFibre{}})
	nestedMsgPaymentPromiseTimeout := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&fibretypes.MsgPaymentPromiseTimeout{}})
	nestedMsgDepositToEscrow := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&fibretypes.MsgDepositToEscrow{}})
	nestedMsgRequestWithdrawal := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&fibretypes.MsgRequestWithdrawal{}})

	proposalWithMsgSend := newMsgSubmitProposal(t, &banktypes.MsgSend{})
	proposalWithMsgExec := newMsgSubmitProposal(t, &msgExec)
	proposalWithNestedProposal := newMsgSubmitProposal(t, newMsgSubmitProposal(t, &banktypes.MsgSend{}))
	proposalWithPayForBlobs := newMsgSubmitProposal(t, &blobtypes.MsgPayForBlobs{})
	proposalWithPayForFibre := newMsgSubmitProposal(t, &fibretypes.MsgPayForFibre{})
	msgExecWithProposal := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{newMsgSubmitProposal(t, &banktypes.MsgSend{})})

	tests := []struct {
		name    string
		msg     sdk.Msg
		wantErr error
	}{
		{
			name:    "Accept msgExec",
			msg:     &msgExec,
			wantErr: nil,
		},
		{
			name:    "Reject nestedMsgExec",
			msg:     &nestedMsgExec,
			wantErr: sdkerrors.ErrNotSupported,
		},
		{
			name:    "Reject nestedMsgPayForBlobs",
			msg:     &nestedMsgPayForBlobs,
			wantErr: sdkerrors.ErrNotSupported,
		},
		{
			name:    "Reject nestedMsgPayForFibre",
			msg:     &nestedMsgPayForFibre,
			wantErr: sdkerrors.ErrNotSupported,
		},
		{
			name:    "Reject nestedMsgPaymentPromiseTimeout",
			msg:     &nestedMsgPaymentPromiseTimeout,
			wantErr: sdkerrors.ErrNotSupported,
		},
		{
			name:    "Reject nestedMsgDepositToEscrow",
			msg:     &nestedMsgDepositToEscrow,
			wantErr: sdkerrors.ErrNotSupported,
		},
		{
			name:    "Reject nestedMsgRequestWithdrawal",
			msg:     &nestedMsgRequestWithdrawal,
			wantErr: sdkerrors.ErrNotSupported,
		},
		{
			name:    "Accept top-level MsgPayForBlobs",
			msg:     &blobtypes.MsgPayForBlobs{},
			wantErr: nil,
		},
		{
			name:    "Accept top-level MsgPayForFibre",
			msg:     &fibretypes.MsgPayForFibre{},
			wantErr: nil,
		},
		{
			name:    "Accept proposal with MsgSend",
			msg:     proposalWithMsgSend,
			wantErr: nil,
		},
		{
			name:    "Reject proposal with MsgPayForBlobs",
			msg:     proposalWithPayForBlobs,
			wantErr: sdkerrors.ErrNotSupported,
		},
		{
			name:    "Reject proposal with MsgPayForFibre",
			msg:     proposalWithPayForFibre,
			wantErr: sdkerrors.ErrNotSupported,
		},
		{
			name:    "Reject proposal with MsgExec",
			msg:     proposalWithMsgExec,
			wantErr: sdkerrors.ErrNotSupported,
		},
		{
			name:    "Reject proposal with nested proposal",
			msg:     proposalWithNestedProposal,
			wantErr: sdkerrors.ErrNotSupported,
		},
		{
			name:    "Reject MsgExec with proposal",
			msg:     &msgExecWithProposal,
			wantErr: sdkerrors.ErrNotSupported,
		},
	}

	decorator := ante.NewNestedMsgDecorator()
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	anteHandler := sdk.ChainAnteDecorators(decorator)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ctx := sdk.NewContext(nil, tmproto.Header{}, true, nil)
			txBuilder := cdc.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.msg))

			// Run the ante handler
			_, err := anteHandler(ctx, txBuilder.GetTx(), false)
			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// newMsgSubmitProposal creates a proposal from msgs.
func newMsgSubmitProposal(t *testing.T, msgs ...sdk.Msg) *govv1.MsgSubmitProposal {
	t.Helper()
	proposal := &govv1.MsgSubmitProposal{}
	require.NoError(t, proposal.SetMsgs(msgs))
	return proposal
}
