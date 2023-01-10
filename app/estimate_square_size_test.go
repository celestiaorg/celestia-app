package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
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
			ptxs := generateMixedParsedTxs(tt.normalTxs, tt.pfbCount, tt.pfbSize)
			res, _ := estimateSquareSize(ptxs)
			assert.Equal(t, tt.expectedSquareSize, res)
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
		{"one large pfb large block", appconsts.DefaultMaxSquareSize, 1, 1_000_000},
		{"one hundred large pfb large block", appconsts.DefaultMaxSquareSize, 100, 100_000},
		{"one hundred large pfb medium block", appconsts.DefaultMaxSquareSize / 2, 100, 100_000},
		{"ten thousand small pfb large block", appconsts.DefaultMaxSquareSize, 10_000, 1},
		{"ten thousand small pfb medium block", appconsts.DefaultMaxSquareSize / 2, 10_000, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ptxs := generateMixedParsedTxs(0, tt.pfbCount, tt.pfbSize)
			res := estimatePFBTxSharesUsed(tt.squareSize, ptxs)

			// check that our estimate is always larger or equal to the number
			// of compact shares actually used
			txs := make([]coretypes.Tx, len(ptxs))
			for i, ptx := range ptxs {
				if len(ptx.normalTx) != 0 {
					txs[i] = ptx.normalTx
					continue
				}
				wPFBTx, err := coretypes.MarshalIndexWrapper(
					uint32(tt.squareSize*tt.squareSize),
					ptx.blobTx.Tx,
				)
				require.NoError(t, err)
				txs[i] = wPFBTx
			}
			_, pfbTxShares := shares.SplitTxs(txs)
			assert.LessOrEqual(t, len(pfbTxShares), res)
		})
	}
}

func Test_estimateTxSharesUsed(t *testing.T) {
	type testCase struct {
		name string
		ptxs []parsedTx
		want int
	}
	testCases := []testCase{
		{"empty", []parsedTx{}, 0},
		{"one tx", generatedNormalParsedTxs(1), 1},
	}
	for _, tc := range testCases {
		got := estimateTxSharesUsed(tc.ptxs)
		assert.Equal(t, tc.want, got)
	}
}

func Test_estimateCompactShares(t *testing.T) {
	type testCase struct {
		name       string
		totalBytes int
		want       int
	}
	testCases := []testCase{
		{"empty", 0, 0},
		{"one byte", 1, 1},
		{"ten bytes", 10, 1},
		{"one hundred bytes", 100, 1},
		{"exactly one compact share", 495, 1},
		{"just over one compact share", 496, 2},
		{"exactly two compact shares", 994, 2},
		{"just over two compact shares", 995, 3},
		{"exactly three compact shares", 1493, 3},
		{"just over three compact shares", 1494, 4},
		{"one hundred compact shares", 1 + (appconsts.ContinuationCompactShareContentSize * 99), 100},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := estimateCompactShares(tc.totalBytes)
			assert.Equal(t, tc.want, got)
		})
	}
}
