package ante_test

import (
	"testing"

	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"
)

func TestMultisigISMValidatorsDecorator(t *testing.T) {
	validators := func(n int) []string { return make([]string, n) }
	messageID := func(n int) sdk.Msg { return &ismtypes.MsgCreateMessageIdMultisigIsm{Validators: validators(n)} }
	merkleRoot := func(n int) sdk.Msg { return &ismtypes.MsgCreateMerkleRootMultisigIsm{Validators: validators(n)} }
	execWith := func(msg sdk.Msg) sdk.Msg {
		exec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{msg})
		return &exec
	}
	limit := appconsts.MaxMultisigISMValidators

	tests := []struct {
		name    string
		msg     sdk.Msg
		wantErr error
	}{
		{name: "message-id at limit", msg: messageID(limit)},
		{name: "message-id over limit", msg: messageID(limit + 1), wantErr: sdkerrors.ErrInvalidRequest},
		{name: "merkle-root at limit", msg: merkleRoot(limit)},
		{name: "merkle-root over limit", msg: merkleRoot(limit + 1), wantErr: sdkerrors.ErrInvalidRequest},
		{name: "authz at limit", msg: execWith(messageID(limit))},
		{name: "authz over limit", msg: execWith(merkleRoot(limit + 1)), wantErr: sdkerrors.ErrInvalidRequest},
		{name: "proposal at limit", msg: newMsgSubmitProposal(t, merkleRoot(limit))},
		{name: "proposal over limit", msg: newMsgSubmitProposal(t, messageID(limit+1)), wantErr: sdkerrors.ErrInvalidRequest},
	}

	anteHandler := sdk.ChainAnteDecorators(ante.NewMultisigISMValidatorsDecorator())
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
