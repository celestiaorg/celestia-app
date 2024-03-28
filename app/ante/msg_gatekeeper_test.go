package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/ante"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestMsgGateKeeperAnteHandler(t *testing.T) {
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
			name:      "Reject MsgMultiSend",
			msg:       &banktypes.MsgMultiSend{},
			acceptMsg: false,
			version:   1,
		},
		{
			name:      "Reject MsgSend with version 2",
			msg:       &banktypes.MsgSend{},
			acceptMsg: false,
			version:   2,
		},
	}

	msgGateKeeper := ante.NewMsgVersioningGateKeeper(map[uint64]map[string]struct{}{
		1: {
			"/cosmos.bank.v1beta1.MsgSend": {},
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
			allowed, err2 := msgGateKeeper.IsAllowed(ctx, sdk.MsgTypeURL(tc.msg))
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
