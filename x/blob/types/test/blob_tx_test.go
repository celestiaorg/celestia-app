package types_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/rand"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestValidateBlobTx(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := "test"
	signer := types.GenerateKeyringSigner(t, acc)
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{0x01}, appns.NamespaceVersionZeroIDSize))
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
			[]namespace.Namespace{ns1},
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
			name: "invalid transaction, mismatched namespace",
			getTx: func() tmproto.BlobTx {
				rawBtx := validRawBtx()
				btx, _ := coretypes.UnmarshalBlobTx(rawBtx)
				btx.Blobs[0].NamespaceId = namespace.RandomBlobNamespace().ID
				return btx
			},
			expectedErr: types.ErrNamespaceMismatch,
		},
		{
			name: "invalid transaction, no pfb",
			getTx: func() tmproto.BlobTx {
				sendTx := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, 1)
				blob, err := types.NewBlob(namespace.RandomBlobNamespace(), rand.Bytes(100), appconsts.ShareVersionZero)
				require.NoError(t, err)
				return tmproto.BlobTx{
					Tx:    sendTx[0],
					Blobs: []*tmproto.Blob{blob},
				}
			},
			expectedErr: types.ErrNoPFB,
		},
		{
			name: "mismatched number of pfbs and blobs",
			getTx: func() tmproto.BlobTx {
				rawBtx := validRawBtx()
				btx, _ := coretypes.UnmarshalBlobTx(rawBtx)
				blob, err := types.NewBlob(namespace.RandomBlobNamespace(), rand.Bytes(100), appconsts.ShareVersionZero)
				require.NoError(t, err)
				btx.Blobs = append(btx.Blobs, blob)
				return btx
			},
			expectedErr: types.ErrBlobSizeMismatch,
		},
		{
			name: "invalid share commitment",
			getTx: func() tmproto.BlobTx {
				blob, err := types.NewBlob(namespace.RandomBlobNamespace(), rand.Bytes(100), appconsts.ShareVersionZero)
				require.NoError(t, err)
				msg, err := types.NewMsgPayForBlobs(
					signerAddr.String(),
					blob,
				)
				require.NoError(t, err)

				badCommit, err := types.CreateCommitment(
					&types.Blob{
						NamespaceVersion: uint32(namespace.RandomBlobNamespace().Version),
						NamespaceId:      namespace.RandomBlobNamespace().ID,
						Data:             rand.Bytes(99),
						ShareVersion:     uint32(appconsts.ShareVersionZero),
					})
				require.NoError(t, err)

				msg.ShareCommitments[0] = badCommit

				builder := signer.NewTxBuilder()
				stx, err := signer.BuildSignedTx(builder, msg)
				require.NoError(t, err)
				rawTx, err := encCfg.TxConfig.TxEncoder()(stx)
				require.NoError(t, err)

				btx := tmproto.BlobTx{
					Tx:    rawTx,
					Blobs: []*tmproto.Blob{blob},
				}
				return btx
			},
			expectedErr: types.ErrInvalidShareCommitment,
		},
		{
			name: "complex transaction with one send and one pfb",
			getTx: func() tmproto.BlobTx {
				signerAddr, err := signer.GetSignerInfo().GetAddress()
				require.NoError(t, err)

				sendMsg := banktypes.NewMsgSend(signerAddr, signerAddr, sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(10))))
				tx := blobfactory.ComplexBlobTxWithOtherMsgs(
					t,
					tmrand.NewRand(),
					signer.Keyring,
					encCfg.TxConfig.TxEncoder(),
					"test",
					acc,
					sendMsg,
				)
				btx, isBlob := coretypes.UnmarshalBlobTx(tx)
				require.True(t, isBlob)
				return btx
			},
			expectedErr: types.ErrMultipleMsgsInBlobTx,
		},
		{
			name: "only send tx",
			getTx: func() tmproto.BlobTx {
				sendtx := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, 1)[0]
				return tmproto.BlobTx{
					Tx: sendtx,
				}
			},
			expectedErr: types.ErrNoPFB,
		},
		{
			name: "normal transaction with two blobs w/ different namespaces",
			getTx: func() tmproto.BlobTx {
				rawBtx := blobfactory.MultiBlobTx(
					t,
					encCfg.TxConfig.TxEncoder(),
					signer,
					0, 0,
					blobfactory.RandBlobsWithNamespace(
						[]namespace.Namespace{namespace.RandomBlobNamespace(), namespace.RandomBlobNamespace()},
						[]int{100, 100})...,
				)
				btx, isBlobTx := coretypes.UnmarshalBlobTx(rawBtx)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with two large blobs w/ different namespaces",
			getTx: func() tmproto.BlobTx {
				rawBtx := blobfactory.MultiBlobTx(
					t,
					encCfg.TxConfig.TxEncoder(),
					signer,
					0, 0,
					blobfactory.RandBlobsWithNamespace(
						[]namespace.Namespace{namespace.RandomBlobNamespace(), namespace.RandomBlobNamespace()},
						[]int{100000, 1000000})...,
				)
				btx, isBlobTx := coretypes.UnmarshalBlobTx(rawBtx)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with two blobs w/ same namespace",
			getTx: func() tmproto.BlobTx {
				ns := namespace.RandomBlobNamespace()
				rawBtx := blobfactory.MultiBlobTx(
					t,
					encCfg.TxConfig.TxEncoder(),
					signer,
					0, 0,
					blobfactory.RandBlobsWithNamespace(
						[]namespace.Namespace{ns, ns},
						[]int{100, 100})...,
				)
				btx, isBlobTx := coretypes.UnmarshalBlobTx(rawBtx)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with one hundred blobs of the same namespace",
			getTx: func() tmproto.BlobTx {
				count := 100
				ns := namespace.RandomBlobNamespace()
				sizes := make([]int, count)
				namespaces := make([]namespace.Namespace, count)
				for i := 0; i < count; i++ {
					sizes[i] = 100
					namespaces[i] = ns
				}
				rawBtx := blobfactory.MultiBlobTx(
					t,
					encCfg.TxConfig.TxEncoder(),
					signer,
					0, 0,
					blobfactory.RandBlobsWithNamespace(
						namespaces,
						sizes,
					)...)
				btx, isBlobTx := coretypes.UnmarshalBlobTx(rawBtx)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := types.ValidateBlobTx(encCfg.TxConfig, tt.getTx())
			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr, tt.name)
			}
		})
	}
}
