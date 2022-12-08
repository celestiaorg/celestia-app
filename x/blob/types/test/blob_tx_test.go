package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestProcessBlobTx(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := "test"
	signer := types.GenerateKeyringSigner(t, acc)
	signerAddr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)

	type test struct {
		name        string
		getTx       func() tmproto.BlobTx
		expectedErr error
	}

	validRawBtx := func() []byte {
		btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			encCfg.TxConfig.TxEncoder(),
			signer,
			[][]byte{
				{1, 1, 1, 1, 1, 1, 1, 1},
			},
			[]int{10},
		)[0]
		return btx
	}

	tests := []test{
		{
			name: "normal transaction",
			getTx: func() tmproto.BlobTx {
				rawBtx := validRawBtx()
				btx, _ := coretypes.UnmarshalBlobTx(rawBtx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "ivalid transaction, mismatched namespace",
			getTx: func() tmproto.BlobTx {
				rawBtx := validRawBtx()
				btx, _ := coretypes.UnmarshalBlobTx(rawBtx)
				btx.Blobs[0].NamespaceId = appconsts.TxNamespaceID
				return btx
			},
			expectedErr: types.ErrNamespaceMismatch,
		},
		{
			name: "ivalid transaction, no pfb",
			getTx: func() tmproto.BlobTx {
				sendTx := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, 1)
				blob, err := types.NewBlob(namespace.RandomBlobNamespace(), rand.Bytes(100))
				require.NoError(t, err)
				return tmproto.BlobTx{
					Tx:    sendTx[0],
					Blobs: []*tmproto.Blob{blob},
				}
			},
			expectedErr: types.ErrNoPFBInBlobTx,
		},
		{
			name: "mismatched number of pfbs and blobs",
			getTx: func() tmproto.BlobTx {
				rawBtx := validRawBtx()
				btx, _ := coretypes.UnmarshalBlobTx(rawBtx)
				blob, err := types.NewBlob(namespace.RandomBlobNamespace(), rand.Bytes(100))
				require.NoError(t, err)
				btx.Blobs = append(btx.Blobs, blob)
				return btx
			},
			expectedErr: types.ErrMismatchedNumberOfPFBorBlob,
		},
		{
			name: "invalid share commitment",
			getTx: func() tmproto.BlobTx {
				rawblob := rand.Bytes(100)
				msg, err := types.NewMsgPayForBlob(
					signerAddr.String(),
					namespace.RandomBlobNamespace(),
					rawblob,
				)

				badCommit, err := types.CreateCommitment(
					namespace.RandomBlobNamespace(),
					rand.Bytes(99),
					appconsts.ShareVersionZero,
				)
				require.NoError(t, err)

				msg.ShareCommitment = badCommit

				builder := signer.NewTxBuilder()
				stx, err := signer.BuildSignedTx(builder, msg)
				require.NoError(t, err)
				rawTx, err := encCfg.TxConfig.TxEncoder()(stx)
				require.NoError(t, err)

				wblob, err := types.NewBlob(msg.NamespaceId, rawblob)
				require.NoError(t, err)
				btx := tmproto.BlobTx{
					Tx:    rawTx,
					Blobs: []*tmproto.Blob{wblob},
				}
				return btx
			},
			expectedErr: types.ErrInvalidShareCommit,
		},
	}

	for _, tt := range tests {
		_, err := types.ProcessBlobTx(encCfg.TxConfig, tt.getTx())
		if tt.expectedErr != nil {
			assert.ErrorIs(t, tt.expectedErr, err, tt.name)
		}
	}
}
