package shares

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestCompactShareWriter(t *testing.T) {
	// note that this test is mainly for debugging purposes, the main round trip
	// tests occur in TestMerge and Test_processCompactShares
	w := NewCompactShareSplitter(appconsts.TxNamespaceID)
	txs := generateRandomCompactShares(33, 200)
	for _, tx := range txs {
		rawTx, _ := MarshalDelimitedTx(tx)
		w.WriteBytes(rawTx)
	}
	resShares := w.Export()
	rawResTxs, err := parseCompactShares(resShares.RawShares())
	resTxs := coretypes.ToTxs(rawResTxs)
	require.NoError(t, err)

	assert.Equal(t, txs, resTxs)
}

func Test_parseDelimiter(t *testing.T) {
	for i := uint64(0); i < 100; i++ {
		tx := generateRandomCompactShares(1, int(i))[0]
		input, err := MarshalDelimitedTx(tx)
		if err != nil {
			panic(err)
		}
		res, txLen, err := ParseDelimiter(input)
		if err != nil {
			panic(err)
		}
		assert.Equal(t, i, txLen)
		assert.Equal(t, []byte(tx), res)
	}
}

func TestFuzz_processCompactShares(t *testing.T) {
	t.Skip()
	// run random shares through processCompactShares for a minute
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			Test_processCompactShares(t)
		}
	}
}

func Test_processCompactShares(t *testing.T) {
	// exactTxShareSize is the length of tx that will fit exactly into a single
	// share, accounting for namespace id and the length delimiter prepended to
	// each tx
	const exactTxShareSize = appconsts.CompactShareContentSize - 1

	type test struct {
		name    string
		txSize  int
		txCount int
	}

	// each test is ran twice, once using txSize as an exact size, and again
	// using it as a cap for randomly sized txs
	tests := []test{
		{"single small tx", 10, 1},
		{"many small txs", 10, 10},
		{"single big tx", 1000, 1},
		{"many big txs", 1000, 10},
		{"single exact size tx", exactTxShareSize, 1},
		{"many exact size txs", exactTxShareSize, 10},
	}

	for _, tc := range tests {
		tc := tc

		// run the tests with identically sized txs
		t.Run(fmt.Sprintf("%s idendically sized ", tc.name), func(t *testing.T) {
			txs := generateRandomCompactShares(tc.txCount, tc.txSize)

			shares := SplitTxs(txs)

			parsedTxs, err := parseCompactShares(shares)
			if err != nil {
				t.Error(err)
			}

			// check that the data parsed is identical
			for i := 0; i < len(txs); i++ {
				assert.Equal(t, []byte(txs[i]), parsedTxs[i])
			}
		})

		// run the same tests using randomly sized txs with caps of tc.txSize
		t.Run(fmt.Sprintf("%s randomly sized", tc.name), func(t *testing.T) {
			txs := generateRandomlySizedCompactShares(tc.txCount, tc.txSize)

			shares := SplitTxs(txs)

			parsedTxs, err := parseCompactShares(shares)
			if err != nil {
				t.Error(err)
			}

			// check that the data parsed is identical to the original
			for i := 0; i < len(txs); i++ {
				assert.Equal(t, []byte(txs[i]), parsedTxs[i])
			}
		})
	}
}
