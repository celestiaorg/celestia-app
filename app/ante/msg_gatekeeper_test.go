package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/ante"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

var (
	nestedBankSend  = authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&banktypes.MsgSend{}})      // allowed in v1 and v3
	nestedMultiSend = authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&banktypes.MsgMultiSend{}}) // allowed in v3
	nestedMsgExec   = authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&nestedBankSend})           // allowed in v1

	acceptedList = map[uint64]map[string]struct{}{
		1: {
			"/cosmos.authz.v1beta1.MsgExec": {},
			"/cosmos.bank.v1beta1.MsgSend":  {},
		},
		2: {
			"/cosmos.authz.v1beta1.MsgExec": {},
		},
		3: {
			"/cosmos.authz.v1beta1.MsgExec":     {},
			"/cosmos.bank.v1beta1.MsgSend":      {},
			"/cosmos.bank.v1beta1.MsgMultiSend": {},
		},
	}
)

func TestMsgGateKeeperAnteHandler(t *testing.T) {
	tests := []struct {
		name       string
		msg        sdk.Msg
		appVersion uint64
		wantErr    error
	}{
		{
			name:       "Accept MsgSend in v1",
			msg:        &banktypes.MsgSend{},
			appVersion: 1,
		},
		{
			name:       "Reject MsgSend in v2",
			msg:        &banktypes.MsgSend{},
			appVersion: 2,
			wantErr:    sdkerrors.ErrNotSupported,
		},
		{
			name:       "Accept MsgSend in v3",
			msg:        &banktypes.MsgSend{},
			appVersion: 3,
		},
		{
			name:       "Accept nestedBankSend in v1",
			msg:        &nestedBankSend,
			appVersion: 1,
		},
		{
			name:       "Reject nestedBankSend in v2",
			msg:        &nestedBankSend,
			appVersion: 2,
			wantErr:    sdkerrors.ErrNotSupported,
		},
		{
			name:       "Accept nestedBankSend in v3",
			msg:        &nestedBankSend,
			appVersion: 3,
		},
		{
			name:       "Reject MsgMultiSend in v1",
			msg:        &banktypes.MsgMultiSend{},
			appVersion: 1,
			wantErr:    sdkerrors.ErrNotSupported,
		},
		{
			name:       "Reject MsgMultiSend in v2",
			msg:        &banktypes.MsgMultiSend{},
			appVersion: 2,
			wantErr:    sdkerrors.ErrNotSupported,
		},
		{
			name:       "Accept MsgMultiSend in v3",
			msg:        &banktypes.MsgMultiSend{},
			appVersion: 3,
		},
		{
			name:       "Reject nestedMultiSend in v1",
			msg:        &nestedMultiSend,
			appVersion: 1,
			wantErr:    sdkerrors.ErrNotSupported,
		},
		{
			name:       "Reject nestedMultiSend in v2",
			msg:        &nestedMultiSend,
			appVersion: 2,
			wantErr:    sdkerrors.ErrNotSupported,
		},
		{
			name:       "Accept nestedMultiSend in v3",
			msg:        &nestedMultiSend,
			appVersion: 3,
		},
		{
			name:       "Accept nestedMsgExec in v1",
			msg:        &nestedMsgExec,
			appVersion: 1,
		},
		{
			name:       "Reject nestedMsgExec in v2",
			msg:        &nestedMsgExec,
			appVersion: 2,
			wantErr:    sdkerrors.ErrNotSupported,
		},
		{
			name:       "Reject nestedMsgExec in v3",
			msg:        &nestedMsgExec,
			appVersion: 3,
			wantErr:    sdkerrors.ErrNotSupported,
		},
	}

	msgGateKeeper := ante.NewMsgVersioningGateKeeper(acceptedList)
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	anteHandler := sdk.ChainAnteDecorators(msgGateKeeper)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.NewContext(nil, tmproto.Header{Version: version.Consensus{App: tc.appVersion}}, false, nil)
			txBuilder := cdc.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.msg))

			_, err := anteHandler(ctx, txBuilder.GetTx(), false)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestIsAllowed(t *testing.T) {
	type testCase struct {
		name       string
		msg        sdk.Msg
		appVersion uint64
		want       bool
	}
	testCases := []testCase{
		{
			name:       "Accept MsgExec in v1",
			msg:        &authz.MsgExec{},
			appVersion: 1,
			want:       true,
		},
		{
			name:       "Accept MsgExec in v2",
			msg:        &authz.MsgExec{},
			appVersion: 2,
			want:       true,
		},
		{
			name:       "Accept MsgExec in v3",
			msg:        &authz.MsgExec{},
			appVersion: 3,
			want:       true,
		},
		{
			name:       "Accept MsgSend in v1",
			msg:        &banktypes.MsgSend{},
			appVersion: 1,
			want:       true,
		},
		{
			name:       "Reject MsgSend in v2",
			msg:        &banktypes.MsgSend{},
			appVersion: 2,
			want:       false,
		},
		{
			name:       "Accept MsgSend in v3",
			msg:        &banktypes.MsgSend{},
			appVersion: 3,
			want:       true,
		},
		{
			name:       "Reject MsgMultiSend in v1",
			msg:        &banktypes.MsgMultiSend{},
			appVersion: 1,
			want:       false,
		},
		{
			name:       "Reject MsgMultiSend in v2",
			msg:        &banktypes.MsgMultiSend{},
			appVersion: 2,
			want:       false,
		},
		{
			name:       "Accept MsgMultiSend in v3",
			msg:        &banktypes.MsgMultiSend{},
			appVersion: 3,
			want:       true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msgGateKeeper := ante.NewMsgVersioningGateKeeper(acceptedList)
			cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
			ctx := sdk.NewContext(nil, tmproto.Header{Version: version.Consensus{App: tc.appVersion}}, false, nil)

			txBuilder := cdc.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.msg))

			got, _ := msgGateKeeper.IsAllowed(ctx, sdk.MsgTypeURL(tc.msg))
			assert.Equal(t, tc.want, got)
		})
	}
}
