package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func Test_estimateSquareSize(t *testing.T) {
	type test struct {
		name                  string
		normalTxs             int
		wPFDCount, messgeSize int
		expectedSize          uint64
	}
	tests := []test{
		{"empty block minimum square size", 0, 0, 0, appconsts.MinSquareSize},
		{"full block with only txs", 10000, 0, 0, appconsts.MaxSquareSize},
		{"random small block square size 2", 0, 1, appconsts.SparseShareContentSize, 2},
		{"random small block square size 4", 0, 1, appconsts.SparseShareContentSize * 10, 4},
		{"random small block w/ 10 normal txs square size 4", 10, 1, appconsts.SparseShareContentSize, 4},
		{"random small block square size 16", 0, 4, appconsts.SparseShareContentSize * 8, 16},
		{"random medium block square size 32", 0, 50, appconsts.SparseShareContentSize * 4, 32},
		{"full block max square size", 0, 8000, appconsts.SparseShareContentSize, appconsts.MaxSquareSize},
		{"overly full block", 0, 80, appconsts.SparseShareContentSize * 100, appconsts.MaxSquareSize},
		{"one over the perfect estimation edge case", 10, 1, appconsts.SparseShareContentSize * 10, 8},
	}
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := generateKeyringSigner(t, "estimate-key")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txs := generateManyRawWirePFD(t, encConf.TxConfig, signer, tt.wPFDCount, tt.messgeSize)
			txs = append(txs, generateManyRawSendTxs(t, encConf.TxConfig, signer, tt.normalTxs)...)
			parsedTxs := parseTxs(encConf.TxConfig, txs)
			squareSize, totalSharesUsed := estimateSquareSize(parsedTxs, core.EvidenceList{})
			assert.Equal(t, tt.expectedSize, squareSize)

			if totalSharesUsed > int(squareSize*squareSize) {
				parsedTxs = prune(encConf.TxConfig, parsedTxs, totalSharesUsed, int(squareSize))
			}

			processedTxs, messages, err := malleateTxs(encConf.TxConfig, squareSize, parsedTxs, core.EvidenceList{})
			require.NoError(t, err)

			blockData := coretypes.Data{
				Txs:                shares.TxsFromBytes(processedTxs),
				Evidence:           coretypes.EvidenceData{},
				Messages:           coretypes.Messages{MessagesList: shares.MessagesFromProto(messages)},
				OriginalSquareSize: squareSize,
			}

			rawShares, err := shares.Split(blockData, true)
			require.NoError(t, err)
			require.Equal(t, int(squareSize*squareSize), len(rawShares))
		})
	}
}

func Test_pruning(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := generateKeyringSigner(t, "estimate-key")
	txs := generateManyRawSendTxs(t, encConf.TxConfig, signer, 10)
	txs = append(txs, generateManyRawWirePFD(t, encConf.TxConfig, signer, 10, 1000)...)
	parsedTxs := parseTxs(encConf.TxConfig, txs)
	ss, total := estimateSquareSize(parsedTxs, core.EvidenceList{})
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
			"basic with small message", 100,
			[]types.TxBuilderOption{
				types.SetFeeAmount(sdk.NewCoins(coin)),
				types.SetGasLimit(10000000),
			},
		},
		{
			"basic with large message", 10000,
			[]types.TxBuilderOption{
				types.SetFeeAmount(sdk.NewCoins(coin)),
				types.SetGasLimit(10000000),
			},
		},
		{
			"memo with medium message", 1000,
			[]types.TxBuilderOption{
				types.SetFeeAmount(sdk.NewCoins(coin)),
				types.SetGasLimit(10000000),
				types.SetMemo("Thou damned and luxurious mountain goat."),
			},
		},
		{
			"memo with large message", 100000,
			[]types.TxBuilderOption{
				types.SetFeeAmount(sdk.NewCoins(coin)),
				types.SetGasLimit(10000000),
				types.SetMemo("Thou damned and luxurious mountain goat."),
			},
		},
	}

	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := generateKeyringSigner(t, "estimate-key")
	for _, tt := range tests {
		wpfdTx := generateRawWirePFDTx(
			t,
			encConf.TxConfig,
			namespace.RandomMessageNamespace(),
			tmrand.Bytes(tt.size),
			signer,
			tt.opts...,
		)
		parsedTxs := parseTxs(encConf.TxConfig, [][]byte{wpfdTx})
		res := overEstimateMalleatedTxSize(len(parsedTxs[0].rawTx), tt.size, len(types.AllSquareSizes(tt.size)))
		malleatedTx, _, err := malleateTxs(encConf.TxConfig, 32, parsedTxs, core.EvidenceList{})
		require.NoError(t, err)
		assert.Less(t, len(malleatedTx[0]), res)
	}
}

func Test_calculateCompactShareCount(t *testing.T) {
	type test struct {
		name                  string
		normalTxs             int
		wPFDCount, messgeSize int
	}
	tests := []test{
		{"empty block minimum square size", 0, 0, totalMsgSize(0)},
		{"full block with only txs", 10000, 0, totalMsgSize(0)},
		{"random small block square size 4", 0, 1, totalMsgSize(appconsts.SparseShareContentSize * 2)},
		{"random small block square size 8", 0, 1, (appconsts.SparseShareContentSize * 4)},
		{"random small block w/ 10 normal txs square size 4", 10, 1, totalMsgSize(appconsts.SparseShareContentSize * 8)},
		{"random small block square size 16", 0, 4, totalMsgSize(appconsts.SparseShareContentSize * 8)},
		{"random medium block square size 32", 0, 50, totalMsgSize(appconsts.SparseShareContentSize * 8)},
		{"full block max square size", 0, 8000, totalMsgSize(appconsts.SparseShareContentSize / 2)},
		{"overly full block", 0, 80, totalMsgSize(appconsts.SparseShareContentSize * 100)},
		{"one over the perfect estimation edge case", 10, 1, totalMsgSize(appconsts.SparseShareContentSize + 1)},
	}
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := generateKeyringSigner(t, "estimate-key")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txs := generateManyRawWirePFD(t, encConf.TxConfig, signer, tt.wPFDCount, tt.messgeSize)
			txs = append(txs, generateManyRawSendTxs(t, encConf.TxConfig, signer, tt.normalTxs)...)

			parsedTxs := parseTxs(encConf.TxConfig, txs)
			squareSize, totalSharesUsed := estimateSquareSize(parsedTxs, core.EvidenceList{})

			if totalSharesUsed > int(squareSize*squareSize) {
				parsedTxs = prune(encConf.TxConfig, parsedTxs, totalSharesUsed, int(squareSize))
			}

			malleated, _, err := malleateTxs(encConf.TxConfig, squareSize, parsedTxs, core.EvidenceList{})
			require.NoError(t, err)

			calculatedTxShareCount := calculateCompactShareCount(parsedTxs, core.EvidenceList{}, int(squareSize))

			txShares := shares.SplitTxs(shares.TxsFromBytes(malleated))
			assert.LessOrEqual(t, len(txShares), calculatedTxShareCount, tt.name)
		})
	}
}

// totalMsgSize subtracts the delimiter size from the desired total size. this
// is useful for testing for messages that occupy exactly so many shares.
func totalMsgSize(size int) int {
	return size - shares.DelimLen(uint64(size))
}
