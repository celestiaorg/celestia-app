package shares_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
)

func TestCounterMatchesCompactShareSplitter(t *testing.T) {
	testCases := []struct {
		txs []coretypes.Tx
	}{
		{txs: []coretypes.Tx{newTx(120)}},
		{txs: []coretypes.Tx{newTx(appconsts.FirstCompactShareContentSize - 2)}},
		{txs: []coretypes.Tx{newTx(appconsts.FirstCompactShareContentSize - 1)}},
		{txs: []coretypes.Tx{newTx(appconsts.FirstCompactShareContentSize), newTx(appconsts.ContinuationCompactShareContentSize - 4)}},
		{txs: newTxs(10, 100)},
		{txs: newTxs(1000, 100)},
		{txs: newTxs(100, 1000)},
	}

	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("case%d", idx), func(t *testing.T) {
			writer := shares.NewCompactShareSplitter(appconsts.PayForBlobNamespaceID, appconsts.ShareVersionZero)
			counter := shares.NewCounter()

			sum := 0
			for _, tx := range tc.txs {
				writer.WriteTx(tx)
				diff := counter.Add(len(tx))
				require.Equal(t, writer.Count()-sum, diff)
				sum = writer.Count()
			}
			shares := writer.Export()
			require.Equal(t, len(shares), sum)
			require.Equal(t, len(shares), counter.Size())
		})
	}

	writer := shares.NewCompactShareSplitter(appconsts.PayForBlobNamespaceID, appconsts.ShareVersionZero)
	counter := shares.NewCounter()
	require.Equal(t, counter.Size(), 0)
	require.Equal(t, writer.Count(), counter.Size())
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
