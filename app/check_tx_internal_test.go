package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
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
