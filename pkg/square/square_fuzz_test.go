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
		s, orderedTxs, err := square.Build(txs, appconsts.DefaultMaxSquareSize)
		require.NoError(t, err)
		s2, err := square.Construct(orderedTxs, appconsts.DefaultMaxSquareSize)
		require.NoError(t, err)
		require.True(t, s.Equals(s2))

		cacher := inclusion.NewSubtreeCacher(uint64(s.Size()))
		eds, err := rsmt2d.ComputeExtendedDataSquare(shares.ToBytes(s), appconsts.DefaultCodec(), cacher.Constructor)
		require.NoError(t, err)
		dah := da.NewDataAvailabilityHeader(eds)

		decoder := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig.TxDecoder()

		builder, err := square.NewBuilder(appconsts.DefaultMaxSquareSize, orderedTxs...)
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
				commitment, err := inclusion.GetCommitment(cacher, dah, int(shareIndex), shares.SparseSharesNeeded(pfb.BlobSizes[blobIndex]))
				require.NoError(t, err)
				require.Equal(t, pfb.ShareCommitments[blobIndex], commitment)
			}
		}
	})
}
