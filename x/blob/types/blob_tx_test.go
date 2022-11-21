package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestDecode(t *testing.T) {
	acc := "test account"
	signer := generateKeyringSigner(t, acc)
	encCfg := makeBlobEncodingConfig()
	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)
	blobSize := 1000
	ns := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	b := tmproto.Blob{
		NamespaceId: ns,
		Data:        rand.Bytes(blobSize),
	}
	commitment, err := CreateCommitment(ns, b.Data)
	require.NoError(t, err)
	msgPFB := MsgPayForBlob{
		Signer:          addr.String(),
		NamespaceId:     ns,
		BlobSize:        uint64(blobSize),
		ShareCommitment: commitment,
	}

	sdkTx, err := signer.BuildSignedTx(signer.NewTxBuilder(), &msgPFB)
	require.NoError(t, err)

	rawTx, err := encCfg.TxConfig.TxEncoder()(sdkTx)
	require.NoError(t, err)

	rawProtoBlob, err := b.Marshal()
	require.NoError(t, err)

	bTx := &BlobTx{
		Tx: rawTx,
		Blobs: [][]byte{
			rawProtoBlob,
		},
	}

	rawBTx, err := bTx.Marshal()
	require.NoError(t, err)

	decodedSdkTx, err := encCfg.TxConfig.TxDecoder()(rawBTx)
	require.NoError(t, err)

	err = decodedSdkTx.ValidateBasic()
	require.NoError(t, err)

	msgs := decodedSdkTx.GetMsgs()
	require.Len(t, msgs, 1)

	assert.Equal(t, msgs[0], msgPFB)

}
