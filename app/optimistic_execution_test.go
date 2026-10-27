package app

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestStripBlobTxs(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	encode := func(msg sdk.Msg) []byte {
		builder := txConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msg))
		raw, err := txConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		return raw
	}
	sendTx := encode(&banktypes.MsgSend{FromAddress: "a", ToAddress: "b"})
	pffTx := encode(&fibretypes.MsgPayForFibre{Signer: "a"})
	blob, err := share.NewBlob(share.RandomBlobNamespace(), bytes.Repeat([]byte{1}, 2000), share.ShareVersionZero, nil)
	require.NoError(t, err)
	blobTx, err := blobtx.MarshalBlobTx(sendTx, blob)
	require.NoError(t, err)
	garbage := []byte("not a tx")

	txs := [][]byte{sendTx, blobTx, pffTx, garbage, blobTx}
	input := make([][]byte, len(txs))
	for i, tx := range txs {
		input[i] = bytes.Clone(tx)
	}

	require.Equal(t, [][]byte{sendTx, sendTx, pffTx, garbage, sendTx}, stripBlobTxs(txs))
	require.Equal(t, input, txs, "input must not be mutated")
}
