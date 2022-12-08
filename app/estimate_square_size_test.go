package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

const (
	testEstimateKey = "estimate-key"
)

func Test_estimateSquareSize(t *testing.T) {
	type test struct {
		name                  string
		normalTxs             int
		wPFBCount, messgeSize int
		expectedSize          uint64
	}
	tests := []test{
		{"empty block minimum square size", 0, 0, 0, appconsts.DefaultMinSquareSize},
		{"full block with only txs", 10000, 0, 0, appconsts.DefaultMaxSquareSize},
		{"3 tx shares + 2 blob shares = 5 total shares so square size 4", 0, 1, appconsts.SparseShareContentSize, 4},
		{"random small block square size 4", 0, 1, appconsts.SparseShareContentSize * 10, 4},
		{"random small block w/ 10 normal txs square size 4", 10, 1, appconsts.SparseShareContentSize, 4},
		{"random small block square size 16", 0, 4, appconsts.SparseShareContentSize * 8, 16},
		{"random medium block square size 32", 0, 50, appconsts.SparseShareContentSize * 4, 32},
		{"full block max square size", 0, 5000, appconsts.SparseShareContentSize, appconsts.DefaultMaxSquareSize},
		{"overly full block", 0, 80, appconsts.SparseShareContentSize * 100, appconsts.DefaultMaxSquareSize},
		{"one over the perfect estimation edge case", 10, 1, appconsts.SparseShareContentSize * 10, 8},
	}
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := types.GenerateKeyringSigner(t, testEstimateKey)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txs := GenerateManyRawWirePFB(t, encConf.TxConfig, signer, tt.wPFBCount, tt.messgeSize)
			txs = append(txs, GenerateManyRawSendTxs(t, encConf.TxConfig, signer, tt.normalTxs)...)
			parsedTxs := parseTxs(encConf.TxConfig, txs)
			squareSize, totalSharesUsed := estimateSquareSize(parsedTxs)
			assert.Equal(t, tt.expectedSize, squareSize)

			if totalSharesUsed > int(squareSize*squareSize) {
				parsedTxs = prune(encConf.TxConfig, parsedTxs, totalSharesUsed, int(squareSize))
			}

			processedTxs, blobs, err := malleateTxs(encConf.TxConfig, squareSize, parsedTxs)
			require.NoError(t, err)

			coreBlobs, err := shares.BlobsFromProto(blobs)
			require.NoError(t, err)
			blockData := coretypes.Data{
				Txs:        shares.TxsFromBytes(processedTxs),
				Blobs:      coreBlobs,
				SquareSize: squareSize,
			}

			rawShares, err := shares.Split(blockData, true)
			require.NoError(t, err)
			require.Equal(t, int(squareSize*squareSize), len(rawShares))
		})
	}
}

func Test_pruning(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := types.GenerateKeyringSigner(t, testEstimateKey)
	txs := GenerateManyRawSendTxs(t, encConf.TxConfig, signer, 10)
	txs = append(txs, GenerateManyRawWirePFB(t, encConf.TxConfig, signer, 10, 1000)...)
	parsedTxs := parseTxs(encConf.TxConfig, txs)
	ss, total := estimateSquareSize(parsedTxs)
	nextLowestSS := ss / 2
	prunedTxs := prune(encConf.TxConfig, parsedTxs, total, int(nextLowestSS))
	require.Less(t, len(prunedTxs), len(parsedTxs))
}

func Test_overEstimateMalleatedTxSize(t *testing.T) {
	coin := sdk.Coin{
		Denom:  BondDenom,
		Amount: sdk.NewInt(10),
	}

	type test struct {
		name string
		size int
		opts []types.TxBuilderOption
	}
	tests := []test{
		{
			"basic with small blob", 100,
			[]types.TxBuilderOption{
				types.SetFeeAmount(sdk.NewCoins(coin)),
				types.SetGasLimit(10000000),
			},
		},
		{
			"basic with large blob", 10000,
			[]types.TxBuilderOption{
				types.SetFeeAmount(sdk.NewCoins(coin)),
				types.SetGasLimit(10000000),
			},
		},
		{
			"memo with medium blob", 1000,
			[]types.TxBuilderOption{
				types.SetFeeAmount(sdk.NewCoins(coin)),
				types.SetGasLimit(10000000),
				types.SetMemo("Thou damned and luxurious mountain goat."),
			},
		},
		{
			"memo with large blob", 100000,
			[]types.TxBuilderOption{
				types.SetFeeAmount(sdk.NewCoins(coin)),
				types.SetGasLimit(10000000),
				types.SetMemo("Thou damned and luxurious mountain goat."),
			},
		},
	}

	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := types.GenerateKeyringSigner(t, testEstimateKey)
	for _, tt := range tests {
		wpfbTx := generateRawWirePFB(
			t,
			encConf.TxConfig,
			namespace.RandomBlobNamespace(),
			tmrand.Bytes(tt.size),
			appconsts.ShareVersionZero,
			signer,
			tt.opts...,
		)
		parsedTxs := parseTxs(encConf.TxConfig, [][]byte{wpfbTx})
		res := overEstimateMalleatedTxSize(len(parsedTxs[0].rawTx), tt.size)
		malleatedTx, _, err := malleateTxs(encConf.TxConfig, 32, parsedTxs)
		require.NoError(t, err)
		assert.Less(t, len(malleatedTx[0]), res)
	}
}

func Test_calculateCompactShareCount(t *testing.T) {
	type test struct {
		name                  string
		normalTxs             int
		wPFBCount, messgeSize int
	}
	tests := []test{
		{"empty block minimum square size", 0, 0, totalBlobSize(0)},
		{"full block with only txs", 10000, 0, totalBlobSize(0)},
		{"random small block square size 4", 0, 1, totalBlobSize(appconsts.SparseShareContentSize * 2)},
		{"random small block square size 8", 0, 1, (appconsts.SparseShareContentSize * 4)},
		{"random small block w/ 10 normal txs square size 4", 10, 1, totalBlobSize(appconsts.SparseShareContentSize * 8)},
		{"random small block square size 16", 0, 4, totalBlobSize(appconsts.SparseShareContentSize * 8)},
		{"random medium block square size 32", 0, 50, totalBlobSize(appconsts.SparseShareContentSize * 8)},
		{"full block max square size", 0, 8000, totalBlobSize(appconsts.SparseShareContentSize / 2)},
		{"overly full block", 0, 80, totalBlobSize(appconsts.SparseShareContentSize * 100)},
		{"one over the perfect estimation edge case", 10, 1, totalBlobSize(appconsts.SparseShareContentSize + 1)},
	}
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := types.GenerateKeyringSigner(t, testEstimateKey)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txs := GenerateManyRawWirePFB(t, encConf.TxConfig, signer, tt.wPFBCount, tt.messgeSize)
			txs = append(txs, GenerateManyRawSendTxs(t, encConf.TxConfig, signer, tt.normalTxs)...)

			parsedTxs := parseTxs(encConf.TxConfig, txs)
			squareSize, totalSharesUsed := estimateSquareSize(parsedTxs)

			if totalSharesUsed > int(squareSize*squareSize) {
				parsedTxs = prune(encConf.TxConfig, parsedTxs, totalSharesUsed, int(squareSize))
			}

			malleated, _, err := malleateTxs(encConf.TxConfig, squareSize, parsedTxs)
			require.NoError(t, err)

			calculatedTxShareCount := calculateCompactShareCount(parsedTxs, int(squareSize))

			txShares := shares.SplitTxs(shares.TxsFromBytes(malleated))
			assert.LessOrEqual(t, len(txShares), calculatedTxShareCount, tt.name)
		})
	}
}

// totalBlobSize subtracts the delimiter size from the desired total size. this
// is useful for testing for blobs that occupy exactly so many shares.
func totalBlobSize(size int) int {
	return size - shares.DelimLen(uint64(size))
}
