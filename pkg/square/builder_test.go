package square_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestBuilderSquareSizeEstimation(t *testing.T) {
	type test struct {
		name               string
		normalTxs          int
		pfbCount, pfbSize  int
		expectedSquareSize uint64
	}
	tests := []test{
		{"empty block", 0, 0, 0, appconsts.DefaultMinSquareSize},
		{"one normal tx", 1, 0, 0, 1},
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
			txs := generateMixedTxs(tt.normalTxs, tt.pfbCount, tt.pfbSize)
			square, _, err := square.Construct(txs, appconsts.DefaultMaxSquareSize)
			require.NoError(t, err)
			require.EqualValues(t, tt.expectedSquareSize, square.Size())
		})
	}
}

func generateMixedTxs(normalTxCount, pfbCount, pfbSize int) [][]byte {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	pfbTxs := blobfactory.RandBlobTxs(encCfg.TxConfig.TxEncoder(), pfbCount, pfbSize)
	normieTxs := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, normalTxCount)
	txs := append(append(
		make([]coretypes.Tx, 0, len(pfbTxs)+len(normieTxs)),
		normieTxs...),
		pfbTxs...,
	)
	return coretypes.Txs(txs).ToSliceOfBytes()
}

func TestBuilderRejectsTransactions(t *testing.T) {
	builder, err := square.NewBuilder(2) // 2 x 2 square
	require.NoError(t, err)
	require.False(t, builder.AppendTx(newTx(shares.AvailableBytesFromCompactShares(4)+1)))
	require.True(t, builder.AppendTx(newTx(shares.AvailableBytesFromCompactShares(4))))
	require.False(t, builder.AppendTx(newTx(1)))
}

func TestBuilderRejectsBlobTransactions(t *testing.T) {
}

func TestBuilderInvalidConstructor(t *testing.T) {
	_, err := square.NewBuilder(-4)
	require.Error(t, err)
	_, err = square.NewBuilder(0)
	require.Error(t, err)
	_, err = square.NewBuilder(13)
	require.Error(t, err)
}

func newTx(len int) []byte {
	return bytes.Repeat([]byte{0}, shares.RawTxSize(len))
}
