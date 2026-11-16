package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	"github.com/stretchr/testify/require"
)

func TestModuleQuerySafeRequestsDecorator(t *testing.T) {
	querySafe := func(n int) sdk.Msg {
		requests := make([]*icahosttypes.QueryRequest, n)
		for i := range requests {
			requests[i] = &icahosttypes.QueryRequest{}
		}
		return &icahosttypes.MsgModuleQuerySafe{Requests: requests}
	}
	execWith := func(msg sdk.Msg) sdk.Msg {
		exec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{msg})
		return &exec
	}
	limit := appconsts.MaxModuleQuerySafeRequests

	tests := []struct {
		name    string
		msg     sdk.Msg
		wantErr error
	}{
		{name: "no requests", msg: querySafe(0)},
		{name: "at limit", msg: querySafe(limit)},
		{name: "over limit", msg: querySafe(limit + 1), wantErr: sdkerrors.ErrInvalidRequest},
		{name: "a full block of requests", msg: querySafe(appconsts.MaxSDKMessages - 1), wantErr: sdkerrors.ErrInvalidRequest},
		{name: "authz at limit", msg: execWith(querySafe(limit))},
		{name: "authz over limit", msg: execWith(querySafe(limit + 1)), wantErr: sdkerrors.ErrInvalidRequest},
		{name: "proposal at limit", msg: newMsgSubmitProposal(t, querySafe(limit))},
		{name: "proposal over limit", msg: newMsgSubmitProposal(t, querySafe(limit+1)), wantErr: sdkerrors.ErrInvalidRequest},
	}

	anteHandler := sdk.ChainAnteDecorators(ante.NewModuleQuerySafeRequestsDecorator())
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			txBuilder := cdc.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.msg))
			_, err := anteHandler(sdk.Context{}, txBuilder.GetTx(), false)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
