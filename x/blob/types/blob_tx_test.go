package types_test

import (
	"bytes"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/app/params"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v8/test/util"
	"github.com/celestiaorg/celestia-app/v8/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v8/test/util/random"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v8/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v8/x/blob/types"
	"github.com/celestiaorg/go-square/v4/inclusion"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/merkle"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
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
					appconsts.Version,
					b,
				)
				require.NoError(t, err)

				anotherBlob, err := share.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(99))
				require.NoError(t, err)
				badCommit, err := inclusion.CreateCommitment(
					anotherBlob,
					merkle.HashFromByteSlices,
					appconsts.SubtreeRootThreshold,
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
				sendMsg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(10))))
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
				for i := range count {
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
			err := types.ValidateBlobTx(encCfg.TxConfig, tt.getTx(), appconsts.SubtreeRootThreshold, appconsts.Version)
			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr, tt.name)
			}
		})
	}
}

func TestValidateBlobTxWithCache(t *testing.T) {
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	namespace1, err := share.NewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	require.NoError(t, err)

	accounts := []string{"a", "b", "c", "d", "e"}
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)

	signers := make([]*user.Signer, len(accounts))
	for i, account := range accounts {
		fetchedAcc := testutil.DirectQueryAccount(testApp, testfactory.GetAddress(kr, account))
		signers[i] = createTestSigner(t, kr, account, encodingConfig.TxConfig, fetchedAcc.GetAccountNumber())
	}

	t.Run("cached blob tx uses lightweight validation", func(t *testing.T) {
		blobTxBytes := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			signers[0],
			[]share.Namespace{namespace1},
			[]int{100},
		)[0]

		blobTx, isBlobTx, err := tx.UnmarshalBlobTx(blobTxBytes)
		require.NoError(t, err)
		require.True(t, isBlobTx)

		resp, err := testApp.CheckTx(&abci.RequestCheckTx{
			Type: abci.CheckTxType_New,
			Tx:   blobTxBytes,
		})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)

		fromCache, err := testApp.ValidateBlobTxWithCache(blobTx)
		require.NoError(t, err)
		assert.True(t, fromCache, "expected validation from cache")
	})

	t.Run("non-cached blob tx uses full validation", func(t *testing.T) {
		blobTxBytes := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			signers[1],
			[]share.Namespace{namespace1},
			[]int{100},
		)[0]

		blobTx, isBlobTx, err := tx.UnmarshalBlobTx(blobTxBytes)
		require.NoError(t, err)
		require.True(t, isBlobTx)

		fromCache, err := testApp.ValidateBlobTxWithCache(blobTx)
		require.NoError(t, err)
		assert.False(t, fromCache, "expected validation without cache")
	})

	t.Run("cached blob tx with invalid commitment uses full validation", func(t *testing.T) {
		validBlobTxBytes := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			signers[2],
			[]share.Namespace{namespace1},
			[]int{100},
		)[0]

		resp, err := testApp.CheckTx(&abci.RequestCheckTx{
			Type: abci.CheckTxType_New,
			Tx:   validBlobTxBytes,
		})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)

		blobTx, _, err := tx.UnmarshalBlobTx(validBlobTxBytes)
		require.NoError(t, err)

		newBlob, err := share.NewBlob(share.RandomBlobNamespace(), blobTx.Blobs[0].Data(), appconsts.DefaultShareVersion, nil)
		require.NoError(t, err)
		blobTx.Blobs[0] = newBlob

		fromCache, err := testApp.ValidateBlobTxWithCache(blobTx)
		// With modified blobs, Exists returns false so full validation runs (fromCache=false)
		assert.False(t, fromCache, "blobs changed so cache miss, full validation used")
		assert.Error(t, err, "expected error for invalid blob tx")
	})

	t.Run("cached blob tx with modified blobs uses full validation", func(t *testing.T) {
		blobTxBytes := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			signers[4],
			[]share.Namespace{namespace1},
			[]int{100},
		)[0]

		blobTx, isBlobTx, err := tx.UnmarshalBlobTx(blobTxBytes)
		require.NoError(t, err)
		require.True(t, isBlobTx)

		resp, err := testApp.CheckTx(&abci.RequestCheckTx{
			Type: abci.CheckTxType_New,
			Tx:   blobTxBytes,
		})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)

		// replace the blob with a different one
		blob, err := share.NewBlob(share.RandomBlobNamespace(), blobTx.Blobs[0].Data(), appconsts.DefaultShareVersion, nil)
		require.NoError(t, err)
		blobTx.Blobs[0] = blob

		fromCache, err := testApp.ValidateBlobTxWithCache(blobTx)
		// With modified blobs, Exists returns false so full validation runs (fromCache=false)
		require.Error(t, err)
		require.ErrorContains(t, err, "namespace of blob and its respective MsgPayForBlobs differ")
		assert.False(t, fromCache, "blobs changed so cache miss, full validation used")
	})

	t.Run("cache is cleaned after FinalizeBlock", func(t *testing.T) {
		blobTxBytes := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			signers[3],
			[]share.Namespace{namespace1},
			[]int{100},
		)[0]

		blobTx, isBlobTx, err := tx.UnmarshalBlobTx(blobTxBytes)
		require.NoError(t, err)
		require.True(t, isBlobTx)

		resp, err := testApp.CheckTx(&abci.RequestCheckTx{
			Type: abci.CheckTxType_New,
			Tx:   blobTxBytes,
		})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)

		fromCache, err := testApp.ValidateBlobTxWithCache(blobTx)
		require.NoError(t, err)
		assert.True(t, fromCache, "expected validation from cache before FinalizeBlock")

		// finalize block to clean the cache
		_, err = testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Txs:    [][]byte{blobTx.Tx},
			Time:   time.Now(),
			Height: 2,
		})
		require.NoError(t, err)

		// verify transaction is no longer in cache
		fromCache, err = testApp.ValidateBlobTxWithCache(blobTx)
		require.NoError(t, err)
		assert.False(t, fromCache, "expected validation without cache after FinalizeBlock")
	})
}

func createTestSigner(t *testing.T, kr keyring.Keyring, accountName string, enc client.TxConfig, accNum uint64) *user.Signer {
	t.Helper()

	signer, err := user.NewSigner(kr, enc, testutil.ChainID, user.NewAccount(accountName, accNum, 0))
	require.NoError(t, err)
	return signer
}
