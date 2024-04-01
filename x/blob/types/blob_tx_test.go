package types_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/go-square/blob"
	"github.com/celestiaorg/go-square/inclusion"
	"github.com/celestiaorg/go-square/merkle"
	"github.com/celestiaorg/go-square/namespace"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestNewBlob(t *testing.T) {
	rawBlob := []byte{1}
	validBlob, err := types.NewBlob(namespace.RandomBlobNamespace(), rawBlob, appconsts.ShareVersionZero)
	require.NoError(t, err)
	require.Equal(t, validBlob.Data, rawBlob)

	_, err = types.NewBlob(namespace.TxNamespace, rawBlob, appconsts.ShareVersionZero)
	require.Error(t, err)

	_, err = types.NewBlob(namespace.RandomBlobNamespace(), []byte{}, appconsts.ShareVersionZero)
	require.Error(t, err)
}

func TestValidateBlobTx(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	ns1 := namespace.MustNewV0(bytes.Repeat([]byte{0x01}, namespace.NamespaceVersionZeroIDSize))
	addr := signer.Address()

	type test struct {
		name        string
		getTx       func() *blob.BlobTx
		expectedErr error
	}

	validRawBtx := func() []byte {
		btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			signer,
			[]namespace.Namespace{ns1},
			[]int{10},
		)[0]
		return btx
	}

	tests := []test{
		{
			name: "normal transaction",
			getTx: func() *blob.BlobTx {
				rawBtx := validRawBtx()
				btx, _ := blob.UnmarshalBlobTx(rawBtx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "invalid transaction, mismatched namespace",
			getTx: func() *blob.BlobTx {
				rawBtx := validRawBtx()
				btx, _ := blob.UnmarshalBlobTx(rawBtx)
				btx.Blobs[0].NamespaceId = namespace.RandomBlobNamespace().ID
				return btx
			},
			expectedErr: types.ErrNamespaceMismatch,
		},
		{
			name: "invalid transaction, no pfb",
			getTx: func() *blob.BlobTx {
				sendTx := blobfactory.GenerateManyRawSendTxs(signer, 1)
				b, err := types.NewBlob(namespace.RandomBlobNamespace(), tmrand.Bytes(100), appconsts.ShareVersionZero)
				require.NoError(t, err)
				return &blob.BlobTx{
					Tx:    sendTx[0],
					Blobs: []*blob.Blob{b},
				}
			},
			expectedErr: types.ErrNoPFB,
		},
		{
			name: "mismatched number of pfbs and blobs",
			getTx: func() *blob.BlobTx {
				rawBtx := validRawBtx()
				btx, _ := blob.UnmarshalBlobTx(rawBtx)
				blob, err := types.NewBlob(namespace.RandomBlobNamespace(), tmrand.Bytes(100), appconsts.ShareVersionZero)
				require.NoError(t, err)
				btx.Blobs = append(btx.Blobs, blob)
				return btx
			},
			expectedErr: types.ErrBlobSizeMismatch,
		},
		{
			name: "invalid share commitment",
			getTx: func() *blob.BlobTx {
				b, err := types.NewBlob(namespace.RandomBlobNamespace(), tmrand.Bytes(100), appconsts.ShareVersionZero)
				require.NoError(t, err)
				msg, err := types.NewMsgPayForBlobs(
					addr.String(),
					appconsts.LatestVersion,
					b,
				)
				require.NoError(t, err)

				badCommit, err := inclusion.CreateCommitment(
					&blob.Blob{
						NamespaceVersion: uint32(namespace.RandomBlobNamespace().Version),
						NamespaceId:      namespace.RandomBlobNamespace().ID,
						Data:             tmrand.Bytes(99),
						ShareVersion:     uint32(appconsts.ShareVersionZero),
					},
					merkle.HashFromByteSlices,
					appconsts.DefaultSubtreeRootThreshold,
				)
				require.NoError(t, err)

				msg.ShareCommitments[0] = badCommit

				tx, err := signer.CreateTx([]sdk.Msg{msg})
				require.NoError(t, err)

				rawTx, err := signer.EncodeTx(tx)
				require.NoError(t, err)

				btx := &blob.BlobTx{
					Tx:    rawTx,
					Blobs: []*blob.Blob{b},
				}
				return btx
			},
			expectedErr: types.ErrInvalidShareCommitment,
		},
		{
			name: "complex transaction with one send and one pfb",
			getTx: func() *blob.BlobTx {
				signerAddr := signer.Address()

				sendMsg := banktypes.NewMsgSend(signerAddr, signerAddr, sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(10))))
				tx := blobfactory.ComplexBlobTxWithOtherMsgs(
					t,
					tmrand.NewRand(),
					signer,
					sendMsg,
				)
				btx, isBlob := blob.UnmarshalBlobTx(tx)
				require.True(t, isBlob)
				return btx
			},
			expectedErr: types.ErrMultipleMsgsInBlobTx,
		},
		{
			name: "only send tx",
			getTx: func() *blob.BlobTx {
				sendtx := blobfactory.GenerateManyRawSendTxs(signer, 1)[0]
				return &blob.BlobTx{
					Tx: sendtx,
				}
			},
			expectedErr: types.ErrNoPFB,
		},
		{
			name: "normal transaction with two blobs w/ different namespaces",
			getTx: func() *blob.BlobTx {
				rawBtx, err := signer.CreatePayForBlob(
					blobfactory.RandBlobsWithNamespace(
						[]namespace.Namespace{namespace.RandomBlobNamespace(), namespace.RandomBlobNamespace()},
						[]int{100, 100}),
				)
				require.NoError(t, err)
				btx, isBlobTx := blob.UnmarshalBlobTx(rawBtx)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with two large blobs w/ different namespaces",
			getTx: func() *blob.BlobTx {
				rawBtx, err := signer.CreatePayForBlob(
					blobfactory.RandBlobsWithNamespace(
						[]namespace.Namespace{namespace.RandomBlobNamespace(), namespace.RandomBlobNamespace()},
						[]int{100000, 1000000}),
				)
				require.NoError(t, err)
				btx, isBlobTx := blob.UnmarshalBlobTx(rawBtx)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with two blobs w/ same namespace",
			getTx: func() *blob.BlobTx {
				ns := namespace.RandomBlobNamespace()
				rawBtx, err := signer.CreatePayForBlob(
					blobfactory.RandBlobsWithNamespace(
						[]namespace.Namespace{ns, ns},
						[]int{100, 100}),
				)
				require.NoError(t, err)
				btx, isBlobTx := blob.UnmarshalBlobTx(rawBtx)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with one hundred blobs of the same namespace",
			getTx: func() *blob.BlobTx {
				count := 100
				ns := namespace.RandomBlobNamespace()
				sizes := make([]int, count)
				namespaces := make([]namespace.Namespace, count)
				for i := 0; i < count; i++ {
					sizes[i] = 100
					namespaces[i] = ns
				}
				rawBtx, err := signer.CreatePayForBlob(
					blobfactory.RandBlobsWithNamespace(
						namespaces,
						sizes,
					))
				require.NoError(t, err)
				btx, isBlobTx := blob.UnmarshalBlobTx(rawBtx)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := types.ValidateBlobTx(encCfg.TxConfig, tt.getTx(), appconsts.DefaultSubtreeRootThreshold)
			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr, tt.name)
			}
		})
	}
}
