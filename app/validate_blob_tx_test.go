package app_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/celestiaorg/go-square/v3/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBlobTxWithCache(t *testing.T) {
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	namespace1, err := share.NewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	require.NoError(t, err)

	accounts := []string{"a", "b", "c", "d"}
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

	t.Run("cached blob tx with invalid commitment fails", func(t *testing.T) {
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
		assert.True(t, fromCache, "should have attempted cache validation")
		assert.Error(t, err, "expected error for invalid blob tx")
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
