package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/ante"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestMsgExecDecorator(t *testing.T) {
	msgExec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&banktypes.MsgSend{}})
	nestedMsgExec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&msgExec})
	nestedMsgPayForBlobs := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&blobtypes.MsgPayForBlobs{}})

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
	}

	decorator := ante.NewMsgExecDecorator()
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
