package types_test

import (
	"bytes"
	"testing"

	"cosmossdk.io/math"
	"github.com/cometbft/cometbft/crypto/merkle"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/go-square/v2/inclusion"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/go-square/v2/tx"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

func TestNewV0Blob(t *testing.T) {
	rawBlob := []byte{1}
	validBlob, err := types.NewV0Blob(share.RandomBlobNamespace(), rawBlob)
	require.NoError(t, err)
	require.Equal(t, validBlob.Data(), rawBlob)

	_, err = types.NewV0Blob(share.TxNamespace, rawBlob)
	require.Error(t, err)

	_, err = types.NewV0Blob(share.RandomBlobNamespace(), []byte{})
	require.Error(t, err)
}

func TestValidateBlobTx(t *testing.T) {
	encCfg := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	ns1 := share.MustNewV0Namespace(bytes.Repeat([]byte{0x01}, share.NamespaceVersionZeroIDSize))
	acc := signer.Account(testfactory.TestAccName)
	require.NotNil(t, acc)
	addr := acc.Address()

	type test struct {
		name        string
		getTx       func() *tx.BlobTx
		expectedErr error
	}

	validRawBtx := func() []byte {
		btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			signer,
			[]share.Namespace{ns1},
			[]int{10},
		)[0]
		return btx
	}

	tests := []test{
		{
			name: "normal transaction",
			getTx: func() *tx.BlobTx {
				rawBtx := validRawBtx()
				btx, _, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "invalid transaction, mismatched namespace",
			getTx: func() *tx.BlobTx {
				rawBtx := validRawBtx()
				btx, _, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)

				originalBlob := btx.Blobs[0]
				differentBlob, err := share.NewBlob(share.RandomBlobNamespace(), originalBlob.Data(), originalBlob.ShareVersion(), originalBlob.Signer())
				require.NoError(t, err)

				btx.Blobs[0] = differentBlob
				return btx
			},
			expectedErr: types.ErrNamespaceMismatch,
		},
		{
			name: "invalid transaction, no pfb",
			getTx: func() *tx.BlobTx {
				sendTx := blobfactory.GenerateManyRawSendTxs(signer, 1)
				b, err := types.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(100))
				require.NoError(t, err)
				return &tx.BlobTx{
					Tx:    sendTx[0],
					Blobs: []*share.Blob{b},
				}
			},
			expectedErr: types.ErrNoPFB,
		},
		{
			name: "mismatched number of pfbs and blobs",
			getTx: func() *tx.BlobTx {
				rawBtx := validRawBtx()
				btx, _, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				blob, err := types.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(100))
				require.NoError(t, err)
				btx.Blobs = append(btx.Blobs, blob)
				return btx
			},
			expectedErr: types.ErrBlobSizeMismatch,
		},
		{
			name: "invalid share commitment",
			getTx: func() *tx.BlobTx {
				b, err := types.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(100))
				require.NoError(t, err)
				msg, err := types.NewMsgPayForBlobs(
					addr.String(),
					appconsts.LatestVersion,
					b,
				)
				require.NoError(t, err)

				anotherBlob, err := share.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(99))
				require.NoError(t, err)
				badCommit, err := inclusion.CreateCommitment(
					anotherBlob,
					merkle.HashFromByteSlices,
					appconsts.DefaultSubtreeRootThreshold,
				)
				require.NoError(t, err)

				msg.ShareCommitments[0] = badCommit

				rawTx, _, err := signer.CreateTx([]sdk.Msg{msg})
				require.NoError(t, err)

				btx := &tx.BlobTx{
					Tx:    rawTx,
					Blobs: []*share.Blob{b},
				}
				return btx
			},
			expectedErr: types.ErrInvalidShareCommitment,
		},
		{
			name: "complex transaction with one send and one pfb",
			getTx: func() *tx.BlobTx {
				sendMsg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(10))))
				transaction := blobfactory.ComplexBlobTxWithOtherMsgs(
					t,
					random.New(),
					signer,
					sendMsg,
				)
				btx, ok, err := tx.UnmarshalBlobTx(transaction)
				require.NoError(t, err)
				require.True(t, ok)
				return btx
			},
			expectedErr: types.ErrMultipleMsgsInBlobTx,
		},
		{
			name: "only send tx",
			getTx: func() *tx.BlobTx {
				sendtx := blobfactory.GenerateManyRawSendTxs(signer, 1)[0]
				return &tx.BlobTx{
					Tx: sendtx,
				}
			},
			expectedErr: types.ErrNoPFB,
		},
		{
			name: "normal transaction with two blobs w/ different namespaces",
			getTx: func() *tx.BlobTx {
				rawBtx, _, err := signer.CreatePayForBlobs(acc.Name(),
					blobfactory.RandV0BlobsWithNamespace(
						[]share.Namespace{share.RandomBlobNamespace(), share.RandomBlobNamespace()},
						[]int{100, 100}))
				require.NoError(t, err)
				btx, isBlobTx, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with two large blobs w/ different namespaces",
			getTx: func() *tx.BlobTx {
				rawBtx, _, err := signer.CreatePayForBlobs(acc.Name(),
					blobfactory.RandV0BlobsWithNamespace(
						[]share.Namespace{share.RandomBlobNamespace(), share.RandomBlobNamespace()},
						[]int{100000, 1000000},
					),
				)
				require.NoError(t, err)
				btx, isBlobTx, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with two blobs w/ same namespace",
			getTx: func() *tx.BlobTx {
				ns := share.RandomBlobNamespace()
				rawBtx, _, err := signer.CreatePayForBlobs(acc.Name(),
					blobfactory.RandV0BlobsWithNamespace(
						[]share.Namespace{ns, ns},
						[]int{100, 100},
					),
				)
				require.NoError(t, err)
				btx, isBlobTx, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with one hundred blobs of the same namespace",
			getTx: func() *tx.BlobTx {
				count := 100
				ns := share.RandomBlobNamespace()
				sizes := make([]int, count)
				namespaces := make([]share.Namespace, count)
				for i := 0; i < count; i++ {
					sizes[i] = 100
					namespaces[i] = ns
				}
				rawBtx, _, err := signer.CreatePayForBlobs(acc.Name(),
					blobfactory.RandV0BlobsWithNamespace(
						namespaces,
						sizes,
					))
				require.NoError(t, err)
				btx, isBlobTx, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := types.ValidateBlobTx(encCfg.TxConfig, tt.getTx(), appconsts.DefaultSubtreeRootThreshold, appconsts.LatestVersion)
			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr, tt.name)
			}
		})
	}
}
