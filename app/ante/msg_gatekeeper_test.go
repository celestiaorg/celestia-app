package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	version "github.com/cometbft/cometbft/proto/tendermint/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestMsgGateKeeperAnteHandler(t *testing.T) {
	nestedBankSend := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&banktypes.MsgSend{}})
	nestedMultiSend := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&banktypes.MsgMultiSend{}})

	// Define test cases
	tests := []struct {
		name      string
		msg       sdk.Msg
		acceptMsg bool
		version   uint64
	}{
		{
			name:      "Accept MsgSend",
			msg:       &banktypes.MsgSend{},
			acceptMsg: true,
			version:   1,
		},
		{
			name:      "Accept nested MsgSend",
			msg:       &nestedBankSend,
			acceptMsg: true,
			version:   1,
		},
		{
			name:      "Reject MsgMultiSend",
			msg:       &banktypes.MsgMultiSend{},
			acceptMsg: false,
			version:   1,
		},
		{
			name:      "Reject nested MsgMultiSend",
			msg:       &nestedMultiSend,
			acceptMsg: false,
			version:   1,
		},
		{
			name:      "Reject MsgSend with version 2",
			msg:       &banktypes.MsgSend{},
			acceptMsg: false,
			version:   2,
		},
		{
			name:      "Reject nested MsgSend with version 2",
			msg:       &nestedBankSend,
			acceptMsg: false,
			version:   2,
		},
	}

	msgGateKeeper := ante.NewMsgVersioningGateKeeper(map[uint64]map[string]struct{}{
		1: {
			"/cosmos.bank.v1beta1.MsgSend":  {},
			"/cosmos.authz.v1beta1.MsgExec": {},
		},
		2: {},
	})
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	anteHandler := sdk.ChainAnteDecorators(msgGateKeeper)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.NewContext(nil, tmproto.Header{Version: version.Consensus{App: tc.version}}, false, nil)
			txBuilder := cdc.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.msg))
			_, err := anteHandler(ctx, txBuilder.GetTx(), false)

			msg := tc.msg
			if sdk.MsgTypeURL(msg) == "/cosmos.authz.v1beta1.MsgExec" {
				execMsg, ok := msg.(*authz.MsgExec)
				require.True(t, ok)

				nestedMsgs, err := execMsg.GetMessages()
				require.NoError(t, err)
				msg = nestedMsgs[0]
			}

			allowed, err2 := msgGateKeeper.IsAllowed(ctx, sdk.MsgTypeURL(msg))

			require.NoError(t, err2)
			if tc.acceptMsg {
				require.NoError(t, err, "expected message to be accepted")
				require.True(t, allowed)
			} else {
				require.Error(t, err, "expected message to be rejected")
				require.False(t, allowed)
			}
		})
	}
}
