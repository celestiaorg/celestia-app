package app

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	blobproto "github.com/celestiaorg/go-square/v4/proto/blob/v4"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
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
	// A BlobTx under the legacy rules only: v11 rejects its share version.
	legacyOnly, err := proto.Marshal(&blobproto.BlobTx{
		Tx:     sendTx,
		Blobs:  []*blobproto.BlobProto{{NamespaceId: make([]byte, share.NamespaceIDSize), Data: []byte{1}, ShareVersion: 7}},
		TypeId: blobtx.ProtoBlobTxTypeID,
	})
	require.NoError(t, err)

	txs := [][]byte{sendTx, blobTx, pffTx, garbage, blobTx, legacyOnly}
	input := make([][]byte, len(txs))
	for i, tx := range txs {
		input[i] = bytes.Clone(tx)
	}

	require.Equal(t, [][]byte{sendTx, sendTx, pffTx, garbage, sendTx, sendTx}, stripBlobTxs(txs, 10))
	require.Equal(t, [][]byte{sendTx, sendTx, pffTx, garbage, sendTx, legacyOnly}, stripBlobTxs(txs, 11))
	require.Equal(t, input, txs, "input must not be mutated")
}
