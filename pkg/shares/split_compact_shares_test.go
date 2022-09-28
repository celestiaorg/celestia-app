package shares

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestCount(t *testing.T) {
	type testCase struct {
		transactions   []coretypes.Tx
		wantShareCount int
	}
	testCases := []testCase{
		{transactions: []coretypes.Tx{}, wantShareCount: 0},
		{transactions: []coretypes.Tx{[]byte{0}}, wantShareCount: 1},
		{transactions: []coretypes.Tx{bytes.Repeat([]byte{0}, 100)}, wantShareCount: 1},
		{transactions: []coretypes.Tx{bytes.Repeat([]byte{0}, appconsts.ContinuationCompactShareContentSize+1)}, wantShareCount: 2},
		{transactions: []coretypes.Tx{bytes.Repeat([]byte{0}, appconsts.ContinuationCompactShareContentSize*2+1)}, wantShareCount: 3},
	}
	for _, tc := range testCases {
		css := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersion)
		for _, transaction := range tc.transactions {
			css.WriteTx(transaction)
		}
		got := css.Count()
		if got != tc.wantShareCount {
			t.Errorf("count got %d want %d", got, tc.wantShareCount)
		}
	}
}
