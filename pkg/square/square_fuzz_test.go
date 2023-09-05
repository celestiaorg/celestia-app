package square_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/inclusion"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
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
		signer, err := testnode.NewOfflineSigner()
		require.NoError(t, err)
		txs := GenerateMixedRandomTxs(t, signer, rand, normalTxCount, pfbCount)

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
		dah, err := da.NewDataAvailabilityHeader(eds)
		require.NoError(t, err)

		decoder := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig.TxDecoder()

		builder, err := square.NewBuilder(appconsts.DefaultSquareSizeUpperBound, appconsts.LatestVersion, orderedTxs...)
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
