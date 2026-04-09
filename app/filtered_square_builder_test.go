package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/test/util/blobfactory"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestSeparateTxs(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	normalTx := newNormalTx(t, txConfig)
	blobTx := blobfactory.UnsignedBlobTx(t)

	tests := []struct {
		name     string
		rawTxs   [][]byte
		wantNorm int
		wantBlob int
		wantPFF  int
	}{
		{
			name:     "empty",
			rawTxs:   [][]byte{},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  0,
		},
		{
			name:     "one normal tx",
			rawTxs:   [][]byte{normalTx},
			wantNorm: 1,
			wantBlob: 0,
			wantPFF:  0,
		},
		{
			name:     "one blob tx",
			rawTxs:   [][]byte{blobTx},
			wantNorm: 0,
			wantBlob: 1,
			wantPFF:  0,
		},
		{
			name:     "undecodable tx is dropped",
			rawTxs:   [][]byte{[]byte("garbage")},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			normalTxs, blobTxs, payForFibreTxs := separateTxs(txConfig, tc.rawTxs)
			require.Len(t, normalTxs, tc.wantNorm)
			require.Len(t, blobTxs, tc.wantBlob)
			require.Len(t, payForFibreTxs, tc.wantPFF)
		})
	}
}

// newNormalTx creates an unsigned MsgSend transaction for testing.
func newNormalTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(privKey.PubKey().Address())
	msg := &banktypes.MsgSend{
		FromAddress: addr.String(),
		ToAddress:   addr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("utia", 1)),
	}
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}
