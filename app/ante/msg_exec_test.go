package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/ante"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestMsgExecDecorator(t *testing.T) {
	msgExec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&banktypes.MsgSend{}})
	nestedMsgExec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&msgExec})

	tests := []struct {
		name       string
		msg        sdk.Msg
		appVersion uint64
		wantErr    error
	}{
		{
			name:       "Accept msgExec on v3",
			msg:        &msgExec,
			appVersion: 3,
			wantErr:    nil,
		},
		{
			name:       "Accept nestedMsgExec on v3",
			msg:        &nestedMsgExec,
			appVersion: 3,
			wantErr:    nil,
		},
		{
			name:       "Accept msgExec on v4",
			msg:        &msgExec,
			appVersion: 4,
			wantErr:    nil,
		},
		{
			name:       "Reject nestedMsgExec on v4",
			msg:        &nestedMsgExec,
			appVersion: 4,
			wantErr:    sdkerrors.ErrNotSupported,
		},
	}

	decorator := ante.NewMsgExecDecorator()
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	anteHandler := sdk.ChainAnteDecorators(decorator)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.NewContext(nil, tmproto.Header{Version: version.Consensus{App: tc.appVersion}}, true, nil)
			txBuilder := cdc.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.msg))
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
