package square_test

import (
	"fmt"
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
	)
	f.Add(normalTxCount, pfbCount)
	f.Fuzz(func(t *testing.T, normalTxCount, pfbCount int) {
		// ignore invalid values
		if normalTxCount < 0 || pfbCount < 0 {
			t.Skip()
		}
		encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
		txs := GenerateMixedRandomTxs(t, encCfg.TxConfig, normalTxCount, pfbCount)

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

func TestSeed(t *testing.T) {
	tmrand.Seed(1)
	var bytes [][]byte
	for i := 0; i < 100; i++ {
		bytes = append(bytes, tmrand.Bytes(1))
	}
	fmt.Println(bytes)
	// [[121] [247] [233] [117] [42] [61] [188] [139] [202] [99] [156] [1] [251] [222] [43] [125] [196] [107] [141] [255] [5] [20] [144] [161] [198] [79] [100] [170] [186] [148] [99] [63] [7] [44] [169] [180] [118] [16] [207] [183] [118] [206] [39] [187] [125] [105] [56] [154] [234] [155] [181] [193] [55] [144] [245] [254] [223] [116] [52] [29] [186] [148] [115] [221] [81] [20] [87] [183] [176] [127] [39] [137] [44] [61] [59] [130] [12] [23] [232] [92] [202] [180] [71] [237] [172] [34] [102] [32] [49] [213] [88] [242] [148] [190] [86] [232] [45] [50] [152] [112]]

	tmrand.Seed(1)
	var nums []int
	for i := 0; i < 100; i++ {
		nums = append(nums, tmrand.Intn(100))
	}
	fmt.Println(nums)
	//[81 87 47 59 81 18 25 40 56 0 94 11 62 89 28 74 11 45 37 6 95 66 28 58 47 47 87 88 90 15 41 8 87 31 29 56 37 31 85 26 13 90 94 63 33 47 78 24 59 53 57 21 89 99 0 5 88 38 3 55 51 10 5 56 66 28 61 2 83 46 63 76 2 18 47 94 77 63 96 20 23 53 37 33 41 59 33 43 91 2 78 36 46 7 40 3 52 43 5 98]
}
