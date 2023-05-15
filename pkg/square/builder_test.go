package square_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	ns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
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
			square, _, err := square.Build(txs, appconsts.DefaultMaxSquareSize)
			require.NoError(t, err)
			require.EqualValues(t, tt.expectedSquareSize, square.Size())
		})
	}
}

func generateMixedTxs(normalTxCount, pfbCount, pfbSize int) [][]byte {
	return shuffle(generateOrderedTxs(normalTxCount, pfbCount, pfbSize))
}

func generateOrderedTxs(normalTxCount, pfbCount, pfbSize int) [][]byte {
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

func shuffle(slice [][]byte) [][]byte {
	for i := range slice {
		j := rand.Intn(i + 1)
		slice[i], slice[j] = slice[j], slice[i]
	}
	return slice
}

func TestBuilderRejectsTransactions(t *testing.T) {
	builder, err := square.NewBuilder(2) // 2 x 2 square
	require.NoError(t, err)
	require.False(t, builder.AppendTx(newTx(shares.AvailableBytesFromCompactShares(4)+1)))
	require.True(t, builder.AppendTx(newTx(shares.AvailableBytesFromCompactShares(4))))
	require.False(t, builder.AppendTx(newTx(1)))
}

func TestBuilderRejectsBlobTransactions(t *testing.T) {
	ns1 := ns.MustNewV0(bytes.Repeat([]byte{1}, ns.NamespaceVersionZeroIDSize))
	testCases := []struct {
		blobSize []int
		added    bool
	}{
		{
			blobSize: []int{shares.AvailableBytesFromSparseShares(3) + 1},
			added:    false,
		},
		{
			blobSize: []int{shares.AvailableBytesFromSparseShares(3)},
			added:    true,
		},
		{
			blobSize: []int{shares.AvailableBytesFromSparseShares(2) + 1, shares.AvailableBytesFromSparseShares(1)},
			added:    false,
		},
		{
			blobSize: []int{shares.AvailableBytesFromSparseShares(1), shares.AvailableBytesFromSparseShares(1)},
			added:    true,
		},
		{
			// fun fact: three blobs increases the size of the PFB to two shares, hence this fails
			blobSize: []int{
				shares.AvailableBytesFromSparseShares(1),
				shares.AvailableBytesFromSparseShares(1),
				shares.AvailableBytesFromSparseShares(1),
			},
			added: false,
		},
	}

	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("case%d", idx), func(t *testing.T) {
			builder, err := square.NewBuilder(2)
			require.NoError(t, err)
			txs := generateBlobTxsWithNamespaces(t, ns1.Repeat(len(tc.blobSize)), [][]int{tc.blobSize})
			require.Len(t, txs, 1)
			blobTx, isBlobTx := coretypes.UnmarshalBlobTx(txs[0])
			require.True(t, isBlobTx)
			require.Equal(t, tc.added, builder.AppendBlobTx(blobTx))
		})
	}
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

func TestBuilderFindTxShareRange(t *testing.T) {
	blockTxs := testfactory.GenerateRandomTxs(5, 900).ToSliceOfBytes()
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	blockTxs = append(blockTxs, blobfactory.RandBlobTxsRandomlySized(encCfg.TxConfig.TxEncoder(), 5, 1000, 10).ToSliceOfBytes()...)
	require.Len(t, blockTxs, 10)

	builder, err := square.NewBuilder(appconsts.DefaultMaxSquareSize, blockTxs...)
	require.NoError(t, err)

	dataSquare, err := builder.Export()
	require.NoError(t, err)
	size := dataSquare.Size() * dataSquare.Size()

	var lastEnd int
	for idx, tx := range blockTxs {
		blobTx, isBlobTx := types.UnmarshalBlobTx(tx)
		if isBlobTx {
			tx = blobTx.Tx
		}
		shareRange, err := builder.FindTxShareRange(idx)
		require.NoError(t, err)
		if idx == 5 {
			// normal txs and PFBs use a different namespace so there
			// can't be any overlap in the index
			require.Greater(t, shareRange.Start, lastEnd-1)
		} else {
			require.GreaterOrEqual(t, shareRange.Start, lastEnd-1)
		}
		require.LessOrEqual(t, shareRange.End, size)
		txShares := dataSquare[shareRange.Start : shareRange.End+1]
		parsedShares, err := rawData(txShares)
		require.NoError(t, err)
		require.True(t, bytes.Contains(parsedShares, tx))
		lastEnd = shareRange.End
	}
}

func rawData(shares []shares.Share) ([]byte, error) {
	var data []byte
	for _, share := range shares {
		rawData, err := share.RawData()
		if err != nil {
			return nil, err
		}
		data = append(data, rawData...)
	}
	return data, nil
}
