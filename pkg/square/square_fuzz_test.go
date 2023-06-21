package square_test

import (
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/stretchr/testify/assert"
	"github.com/tendermint/tendermint/types"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/inclusion"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	apptypes "github.com/celestiaorg/celestia-app/x/blob/types"
	blob "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

// FuzzSquare uses fuzzing to test the following:
// - That neither `Construct` or `Reconstruct` panics
// - That `Construct` never errors
// - That `Deconstruct` extracts transactions from a square identical to those used for its `Construct`ion.
// - That `Reconstruct` never errors from the input of `Construct`'s output
// - That both `Construct` and `Reconstruct` return the same square
// - That the square can be extended and a data availability header can be generated
// - That each share commitment in each PFB can be used to verify the inclusion of the blob it corresponds to.
func FuzzSquare(f *testing.F) {
	var (
		normalTxCount = 12
		pfbCount      = 91
		seed          = int64(3554045230938829713)
	)
	f.Add(normalTxCount, pfbCount, seed)
	f.Fuzz(func(t *testing.T, normalTxCount, pfbCount int, seed int64) {
		// ignore invalid values
		if normalTxCount < 0 || pfbCount < 0 {
			t.Skip()
		}
		encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
		rand := tmrand.NewRand()
		rand.Seed(seed)
		txs := GenerateMixedRandomTxs(t, encCfg.TxConfig, rand, normalTxCount, pfbCount)

		s, orderedTxs, err := square.Build(txs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
		require.NoError(t, err)
		s2, err := square.Construct(orderedTxs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
		require.NoError(t, err)
		require.True(t, s.Equals(s2))
		// check that orderedTxs is a subset of all txs
		require.True(t, contains(txs, orderedTxs))

		// check that the same set of transactions is extracted from the square
		recomputedTxs, err := square.Deconstruct(s2, encCfg.TxConfig.TxDecoder())
		require.NoError(t, err)
		require.Equal(t, orderedTxs, recomputedTxs.ToSliceOfBytes())

		cacher := inclusion.NewSubtreeCacher(uint64(s.Size()))
		eds, err := rsmt2d.ComputeExtendedDataSquare(shares.ToBytes(s), appconsts.DefaultCodec(), cacher.Constructor)
		require.NoError(t, err)
		dah := da.NewDataAvailabilityHeader(eds)

		decoder := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig.TxDecoder()

		builder, err := square.NewBuilder(appconsts.DefaultSquareSizeUpperBound, appconsts.DefaultSubtreeRootThreshold, orderedTxs...)
		require.NoError(t, err)
		totalPfbs := builder.NumPFBs()
		totalNormalTxs := builder.NumTxs() - totalPfbs
		for pfbIndex := 0; pfbIndex < totalPfbs; pfbIndex++ {
			wpfb, err := builder.GetWrappedPFB(pfbIndex + totalNormalTxs)
			require.NoError(t, err)
			tx, err := decoder(wpfb.Tx)
			require.NoError(t, err)

			pfb, ok := tx.GetMsgs()[0].(*blob.MsgPayForBlobs)
			require.True(t, ok)

			for blobIndex, shareIndex := range wpfb.ShareIndexes {
				commitment, err := inclusion.GetCommitment(cacher, dah, int(shareIndex), shares.SparseSharesNeeded(pfb.BlobSizes[blobIndex]), appconsts.DefaultSubtreeRootThreshold)
				require.NoError(t, err)
				require.Equal(t, pfb.ShareCommitments[blobIndex], commitment)
			}
		}
	})
}

// contains checks whether subTxs is a subset of allTxs.
func contains(allTxs [][]byte, subTxs [][]byte) bool {
	// create a map of allTxs
	allTxMap := make(map[string]bool)
	for _, tx := range allTxs {
		allTxMap[string(tx)] = true
	}
	// check that all subTxs are in allTxs
	for _, t := range subTxs {
		if !allTxMap[string(t)] {
			return false
		}
	}
	return true
}

// TestRandMultiBlobTxs tests whether the same random seed produces the same blob txs.
func TestRandMultiBlobTxs_Deterministic(t *testing.T) {
	pfbCount := 10
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decoder := encCfg.TxConfig.TxDecoder()

	rand1 := tmrand.NewRand()
	rand1.Seed(1)
	marshalledBlobTxs1 := blobfactory.RandMultiBlobTxs(t, encCfg.TxConfig.TxEncoder(), rand1, pfbCount)

	rand2 := tmrand.NewRand()
	rand2.Seed(1)
	marshalledBlobTxs2 := blobfactory.RandMultiBlobTxs(t, encCfg.TxConfig.TxEncoder(), rand2, pfbCount)

	// additional checks for the sake of future debugging
	for index := 0; index < pfbCount; index++ {
		blobTx1, isBlob := types.UnmarshalBlobTx(marshalledBlobTxs1[index])
		assert.True(t, isBlob)
		pfbMsgs1, err := decoder(blobTx1.Tx)
		assert.NoError(t, err)

		blobTx2, isBlob := types.UnmarshalBlobTx(marshalledBlobTxs2[index])
		assert.True(t, isBlob)
		pfbMsgs2, err := decoder(blobTx2.Tx)
		assert.NoError(t, err)

		assert.Equal(t, blobTx1.Blobs, blobTx2.Blobs)
		assert.Equal(t, blobTx1.Tx, blobTx2.Tx)
		assert.Equal(t, pfbMsgs1, pfbMsgs2)

		msgs2 := pfbMsgs2.GetMsgs()
		msgs1 := pfbMsgs1.GetMsgs()
		for i, msg := range msgs1 {
			assert.Equal(t, msg, msgs2[i])
		}
	}

	assert.Equal(t, marshalledBlobTxs1, marshalledBlobTxs2)
}
func TestGenerateOrderedRandomTxs_Deterministic(t *testing.T) {
	pfbCount := 10
	noramlCount := 10
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	rand1 := tmrand.NewRand()
	rand1.Seed(1)
	set1 := GenerateOrderedRandomTxs(t, encCfg.TxConfig, rand1, noramlCount, pfbCount)

	rand2 := tmrand.NewRand()
	rand2.Seed(1)
	set2 := GenerateOrderedRandomTxs(t, encCfg.TxConfig, rand2, noramlCount, pfbCount)

	assert.Equal(t, set2, set1)

}

// TestGenerateManyRandomRawSendTxsSameSigner_Determinism ensures that the same seed produces the same txs
func TestGenerateManyRandomRawSendTxsSameSigner_Deterministic(t *testing.T) {
	normalTxCount := 10
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	TxDecoder := encCfg.TxConfig.TxDecoder()

	signer := apptypes.GenerateKeyringSigner(t)

	rand := tmrand.NewRand()
	rand.Seed(1)
	encodedTxs1 := blobfactory.GenerateManyRandomRawSendTxsSameSigner(encCfg.TxConfig, rand, signer, normalTxCount)

	rand2 := tmrand.NewRand()
	rand2.Seed(1)
	encodedTxs2 := blobfactory.GenerateManyRandomRawSendTxsSameSigner(encCfg.TxConfig, rand2, signer, normalTxCount)

	// additional check for the sake of future debugging
	for i := 0; i < normalTxCount; i++ {
		tx1, err := TxDecoder(encodedTxs1[i])
		assert.NoError(t, err)
		assert.NotNil(t, tx1)
		msgs1 := tx1.GetMsgs()

		tx2, err2 := TxDecoder(encodedTxs2[i])
		assert.NoError(t, err2)
		assert.NotNil(t, tx2)
		msgs2 := tx2.GetMsgs()

		assert.Equal(t, msgs1, msgs2)
		assert.Equal(t, tx1, tx2)
	}

	assert.Equal(t, encodedTxs1, encodedTxs2)

}
