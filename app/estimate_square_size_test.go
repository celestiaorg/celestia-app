package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func Test_estimateSquareSize(t *testing.T) {
	type test struct {
		name               string
		normalTxs          int
		pfbCount, pfbSize  int
		expectedSquareSize uint64
	}
	tests := []test{
		{"empty block", 0, 0, 0, appconsts.DefaultMinSquareSize},
		{"one normal tx", 1, 0, 0, appconsts.DefaultMinSquareSize},
		{"one small pfb small block", 0, 1, 100, 2},
		{"mixed small block", 10, 12, 500, 8},
		{"small block 2", 0, 12, 1000, 8},
		{"mixed medium block 2", 10, 20, 10000, 32},
		{"one large pfb large block", 0, 1, 1000000, 64},
		{"one hundred large pfb large block", 0, 100, 100000, appconsts.DefaultMaxSquareSize},
		{"one hundred large pfb medium block", 100, 100, 100000, appconsts.DefaultMaxSquareSize},
		{"mixed transactions large block", 100, 100, 100000, appconsts.DefaultMaxSquareSize},
		{"mixed transactions large block 2", 1000, 1000, 10000, appconsts.DefaultMaxSquareSize},
		{"mostly transactions large block", 10000, 1000, 100, appconsts.DefaultMaxSquareSize},
		{"only small pfb large block", 0, 10000, 1, appconsts.DefaultMaxSquareSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			squareSize, _ := estimateSquareSize(generateMixedTxs(tt.normalTxs, tt.pfbCount, tt.pfbSize))
			assert.EqualValues(t, tt.expectedSquareSize, squareSize)
		})
	}
}

func Test_estimateSquareSize_MultiBlob(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	acc := "account"
	kr := testfactory.GenerateKeyring(acc)
	signer := blobtypes.NewKeyringSigner(kr, acc, "chainid")
	type test struct {
		name                       string
		getBlobSizes               func() [][]int
		expectedSquareSize         uint64
		expectedStartingShareIndex int
	}
	tests := []test{
		{
			"single share multiblob transaction",
			func() [][]int { return [][]int{{4}} },
			2, 1,
		},
		{
			"10 multiblob single share transactions",
			func() [][]int {
				return blobfactory.Repeat([]int{100}, 10)
			},
			8, 7,
		},
		{
			"10 multiblob 2 share transactions",
			func() [][]int {
				return blobfactory.Repeat([]int{1000}, 10)
			},
			8, 7,
		},
		{
			"10 multiblob 4 share transactions",
			func() [][]int {
				return blobfactory.Repeat([]int{2000}, 10)
			},
			16, 7,
		},
		{
			"100 multiblob single share transaction", func() [][]int {
				return [][]int{blobfactory.Repeat(int(100), 100)}
			},
			16, 11,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txs := blobfactory.ManyMultiBlobTxSameSigner(
				t,
				enc.TxConfig.TxEncoder(),
				signer,
				tt.getBlobSizes(),
				0, 0,
			)
			normalTxs, blobTxs := separateTxs(enc.TxConfig, shares.TxsToBytes(txs))
			resSquareSize, resStart := estimateSquareSize(normalTxs, blobTxs)
			require.Equal(t, tt.expectedSquareSize, resSquareSize)
			require.Equal(t, tt.expectedStartingShareIndex, resStart)
		})
	}
}

func Test_estimatePFBTxSharesUsed(t *testing.T) {
	type test struct {
		name              string
		squareSize        uint64
		pfbCount, pfbSize int
	}
	tests := []test{
		{"empty block", appconsts.DefaultMinSquareSize, 0, 0},
		{"one small pfb small block", 4, 1, 100},
		{"one large pfb large block", appconsts.DefaultMaxSquareSize, 1, 100_000},
		{"one hundred large pfb large block", appconsts.DefaultMaxSquareSize, 100, 100_000},
		{"one hundred large pfb medium block", appconsts.DefaultMaxSquareSize / 2, 100, 100_000},
		{"ten thousand small pfb large block", appconsts.DefaultMaxSquareSize, 10_000, 1},
		{"ten thousand small pfb medium block", appconsts.DefaultMaxSquareSize / 2, 10_000, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blobTxs := generateBlobTxsWithNIDs(t, namespace.RandomBlobNamespaces(tt.pfbCount), blobfactory.Repeat([]int{tt.pfbSize}, tt.pfbCount))
			got := estimatePFBTxSharesUsed(tt.squareSize, blobTxs)

			// check that our estimate is always larger or equal to the number
			// of pfbTxShares actually used
			txs := make([]coretypes.Tx, len(blobTxs))
			for i, blobTx := range blobTxs {
				wPFBTx, err := coretypes.MarshalIndexWrapper(
					blobTx.Tx,
					uint32(tt.squareSize*tt.squareSize),
				)
				require.NoError(t, err)
				txs[i] = wPFBTx
			}
			_, pfbTxShares, _ := shares.SplitTxs(txs)
			assert.LessOrEqual(t, len(pfbTxShares), got)
		})
	}
}

func Test_estimateTxSharesUsed(t *testing.T) {
	require.Equal(t, 312, len(generateNormalTxs(3)[2]))
	type testCase struct {
		name string
		txs  [][]byte
		want int
	}
	testCases := []testCase{
		{"empty", [][]byte{}, 0},
		{"one tx", generateNormalTxs(1), 1},             // 1 tx is approximately 312 bytes which fits in 1 share
		{"two txs", generateNormalTxs(2), 2},            // 2 txs is approximately 624 bytes which fits in 2 shares
		{"ten txs", generateNormalTxs(10), 7},           // 10 txs is approximately 3120 bytes which fits in 7 shares
		{"one hundred txs", generateNormalTxs(100), 63}, // 100 txs is approximately 31200 bytes which fits in 63 share
	}
	for _, tc := range testCases {
		got := estimateTxSharesUsed(tc.txs)
		assert.Equal(t, tc.want, got)
	}
}

// The point of this test is to fail if anything to do with the serialization
// of index wrappers change, as changes could lead to tricky bugs.
func Test_expected_maxIndexWrapperOverhead(t *testing.T) {
	assert.Equal(t, 2, maxIndexOverhead(4))
	assert.Equal(t, 5, maxIndexOverhead(128))
	assert.Equal(t, 6, maxIndexOverhead(512))
	assert.Equal(t, 12, maxIndexWrapperOverhead(4))
	assert.Equal(t, 16, maxIndexWrapperOverhead(128))
	assert.Equal(t, 16, maxIndexWrapperOverhead(512))
}

func Test_maxIndexWrapperOverhead(t *testing.T) {
	type test struct {
		squareSize int
		blobs      int
	}
	tests := []test{
		{4, 2},
		{32, 2},
		{128, 1},
		{128, 10},
		{128, 1000},
		{512, 4},
	}
	for i, tt := range tests {
		maxTxLen := tt.squareSize * tt.squareSize * appconsts.ContinuationCompactShareContentSize
		blobLens := make([]uint32, tt.blobs)
		for i := 0; i < tt.blobs; i++ {
			blobLens[i] = uint32(tt.squareSize * tt.squareSize)
		}
		tx := make([]byte, maxTxLen)
		wtx, err := coretypes.MarshalIndexWrapper(tx, blobLens...)
		require.NoError(t, err)

		wrapperOverhead := maxIndexWrapperOverhead(uint64(tt.squareSize))
		indexOverhead := maxIndexOverhead(uint64(tt.squareSize)) * tt.blobs

		assert.LessOrEqual(t, len(wtx)-len(tx), wrapperOverhead+indexOverhead, i)
	}
}
