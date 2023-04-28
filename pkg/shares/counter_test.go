package shares_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
)

func TestCounterMatchesCompactShareSplitter(t *testing.T) {
	testCases := []struct {
		txs []coretypes.Tx
	}{
		{txs: []coretypes.Tx{newTx(120)}},
		{txs: []coretypes.Tx{newTx(appconsts.FirstCompactShareContentSize - 2)}},
		{txs: []coretypes.Tx{newTx(appconsts.FirstCompactShareContentSize - 1)}},
		{txs: []coretypes.Tx{newTx(appconsts.FirstCompactShareContentSize)}},
		{txs: []coretypes.Tx{newTx(appconsts.FirstCompactShareContentSize + 1)}},
		{txs: []coretypes.Tx{newTx(appconsts.FirstCompactShareContentSize), newTx(appconsts.ContinuationCompactShareContentSize - 4)}},
		{txs: newTxs(1000, 100)},
		{txs: newTxs(100, 1000)},
		{txs: newTxs(8931, 77)},
	}

	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("case%d", idx), func(t *testing.T) {
			writer := shares.NewCompactShareSplitter(namespace.PayForBlobNamespace, appconsts.ShareVersionZero)
			counter := shares.NewCompactShareCounter()

			sum := 0
			for _, tx := range tc.txs {
				require.NoError(t, writer.WriteTx(tx))
				diff := counter.Add(len(tx))
				require.Equal(t, writer.Count()-sum, diff)
				sum = writer.Count()
				require.Equal(t, sum, counter.Size())
			}
			shares, err := writer.Export()
			require.NoError(t, err)
			require.Equal(t, len(shares), sum)
			require.Equal(t, len(shares), counter.Size())
		})
	}

	writer := shares.NewCompactShareSplitter(namespace.PayForBlobNamespace, appconsts.ShareVersionZero)
	counter := shares.NewCompactShareCounter()
	require.Equal(t, counter.Size(), 0)
	require.Equal(t, writer.Count(), counter.Size())
}

func TestCompactShareCounterRevert(t *testing.T) {
	counter := shares.NewCompactShareCounter()
	counter.Add(appconsts.FirstCompactShareContentSize - 2)
	counter.Add(1)
	require.Equal(t, counter.Size(), 2)
	counter.Revert()
	require.Equal(t, counter.Size(), 1)
}

func newTx(len int) coretypes.Tx {
	return bytes.Repeat([]byte("a"), len)
}

func newTxs(n int, len int) []coretypes.Tx {
	txs := make([]coretypes.Tx, n)
	for i := 0; i < n; i++ {
		txs[i] = newTx(len)
	}
	return txs
}
