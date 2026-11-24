package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v12/app/encoding"
	fibretypes "github.com/celestiaorg/celestia-app/v12/x/fibre/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// TestSignerDataFromTxMalformedModeInfo verifies that signerDataFromTx returns
// an error instead of panicking on a tx whose SignerInfo has a ModeInfo with
// its oneof (Sum) unset. GetSignaturesV2 panics on such a ModeInfo, so this
// exercises the recover directly, independent of CheckTx's response-code guard.
func TestSignerDataFromTxMalformedModeInfo(t *testing.T) {
	encodingConfig := encoding.MakeConfig(ModuleEncodingRegisters...)

	sendMsg := banktypes.NewMsgSend(nil, nil, nil)
	msgAny, err := codectypes.NewAnyWithValue(sendMsg)
	require.NoError(t, err)

	rawTx := &txtypes.Tx{
		Body: &txtypes.TxBody{Messages: []*codectypes.Any{msgAny}},
		AuthInfo: &txtypes.AuthInfo{
			SignerInfos: []*txtypes.SignerInfo{{
				ModeInfo: &txtypes.ModeInfo{}, // present but oneof (Sum) unset
				Sequence: 0,
			}},
			Fee: &txtypes.Fee{},
		},
		Signatures: [][]byte{{}},
	}
	txBz, err := rawTx.Marshal()
	require.NoError(t, err)

	sdkTx, err := encodingConfig.TxConfig.TxDecoder()(txBz)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_, _, err := signerDataFromTx(sdkTx)
		require.Error(t, err)
	})
}

// TestRecheckCacheable checks that only allowlisted messages with a bounded decode are kept.
func TestRecheckCacheable(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	send := banktypes.NewMsgSend(nil, nil, sdk.NewCoins(sdk.NewInt64Coin("utia", 1)))
	badSend := banktypes.NewMsgSend(nil, nil, sdk.Coins{{}, {}})
	exec := authz.NewMsgExec(nil, []sdk.Msg{send})
	for name, tc := range map[string]struct {
		msgs []sdk.Msg
		want bool
	}{
		"send":         {[]sdk.Msg{send, &fibretypes.MsgPayForFibre{}}, true},
		"invalid send": {[]sdk.Msg{badSend}, false},
		"recv packet":  {[]sdk.Msg{send, &channeltypes.MsgRecvPacket{}}, false},
		"exec":         {[]sdk.Msg{&exec}, false},
	} {
		builder := txConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(tc.msgs...))
		require.Equal(t, tc.want, recheckCacheable(builder.GetTx()), name)
	}
}
