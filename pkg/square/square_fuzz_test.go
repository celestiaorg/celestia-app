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
	blob "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/require"
)

// FuzzSquare uses fuzzing to test the following:
// - That neither `Construct` or `Reconstruct` panics
// - That `Construct` never errors
// - That `Reconstruct` never errors from the input of `Construct`'s output
// - That both `Construct` and `Reconstruct` return the same square
// - That the square can be extended and a data availability header can be generated
// - That each share commitment in each PFB can be used to verify the inclusion of the blob it corresponds to.
func FuzzSquare(f *testing.F) {
	var (
		normalTxCount uint = 12
		pfbCount      uint = 91
		blobsPerPfb   uint = 2
		blobSize      uint = 812
	)
	f.Add(normalTxCount, pfbCount, blobsPerPfb, blobSize)
	f.Fuzz(func(t *testing.T, normalTxCount, pfbCount, blobsPerPfb, blobSize uint) {
		// ignore invalid values
		if pfbCount > 0 && (blobSize == 0 || blobsPerPfb == 0) {
			t.Skip()
		}
		txs := generateMixedTxs(int(normalTxCount), int(pfbCount), int(blobsPerPfb), int(blobSize))
		s, orderedTxs, err := square.Build(txs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
		require.NoError(t, err)
		s2, err := square.Construct(orderedTxs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
		require.NoError(t, err)
		require.True(t, s.Equals(s2))

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

// FuzzSquareDeconstruct tests whether square deconstruction function can correctly deconstruct a block back from a given square.
func FuzzSquareDeconstruct(f *testing.F) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	f.Add(0, 1)
	f.Fuzz(func(t *testing.T, normalTxCount int, pfbCount int) {
		// skip negative values
		if normalTxCount < 0 || pfbCount < 0 {
			t.Skip()
		}
		maxBlobSize := 1000 // @TODO there might be a global constant for this
		allTxs := GenerateOrderedRandomTxs(encCfg.TxConfig, normalTxCount, pfbCount, maxBlobSize)

		// extract those transaction that fit into the block
		builtSquare, blockTxs, err := square.Build(allTxs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
		require.NoError(t, err)

		// check that blockTxs is a subset of allTxs
		require.True(t, contains(allTxs, blockTxs))

		// construct the square
		dataSquare, err := square.Construct(blockTxs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
		require.NoError(t, err)

		require.Equal(t, builtSquare, dataSquare)

		recomputedTxs, err := square.Deconstruct(dataSquare, encCfg.TxConfig.TxDecoder())
		require.NoError(t, err)
		require.Equal(t, len(blockTxs), len(recomputedTxs.ToSliceOfBytes()))
		require.Equal(t, blockTxs, recomputedTxs.ToSliceOfBytes())
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
