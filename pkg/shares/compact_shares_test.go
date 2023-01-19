package shares

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestCompactShareWriter(t *testing.T) {
	// note that this test is mainly for debugging purposes, the main round trip
	// tests occur in TestMerge and Test_processCompactShares
	w := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersionZero)
	txs := testfactory.GenerateRandomTxs(33, 200)
	for _, tx := range txs {
		rawTx, _ := MarshalDelimitedTx(tx)
		w.WriteBytes(rawTx)
	}
	shares := w.Export()
	rawShares := ToBytes(shares)
	rawResTxs, err := parseCompactShares(rawShares, appconsts.SupportedShareVersions)
	resTxs := coretypes.ToTxs(rawResTxs)
	require.NoError(t, err)

	assert.Equal(t, txs, resTxs)
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
	// share, accounting for the tx length delimiter prepended to
	// each tx. Note that the length delimiter can be 1 to 10 bytes (varint) but
	// this test assumes it is 1 byte.
	const exactTxShareSize = appconsts.FirstCompactShareContentSize - 1

	type test struct {
		name    string
		txSize  int
		txCount int
	}

	// each test is ran twice, once using txSize as an exact size, and again
	// using it as a cap for randomly sized txs
	tests := []test{
		{"single small tx", appconsts.ContinuationCompactShareContentSize / 8, 1},
		{"many small txs", appconsts.ContinuationCompactShareContentSize / 8, 10},
		{"single big tx", appconsts.ContinuationCompactShareContentSize * 4, 1},
		{"many big txs", appconsts.ContinuationCompactShareContentSize * 4, 10},
		{"single exact size tx", exactTxShareSize, 1},
		{"many exact size txs", exactTxShareSize, 100},
	}

	for _, tc := range tests {
		tc := tc

		// run the tests with identically sized txs
		t.Run(fmt.Sprintf("%s idendically sized", tc.name), func(t *testing.T) {
			txs := testfactory.GenerateRandomTxs(tc.txCount, tc.txSize)

			shares, _ := SplitTxs(txs)
			rawShares := ToBytes(shares)

			parsedTxs, err := parseCompactShares(rawShares, appconsts.SupportedShareVersions)
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
			txs := testfactory.GenerateRandomlySizedTxs(tc.txCount, tc.txSize)

			shares, _ := SplitTxs(txs)
			rawShares := ToBytes(shares)

			parsedTxs, err := parseCompactShares(rawShares, appconsts.SupportedShareVersions)
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

func TestCompactShareContainsInfoByte(t *testing.T) {
	css := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersionZero)
	txs := testfactory.GenerateRandomTxs(1, appconsts.ContinuationCompactShareContentSize/4)

	for _, tx := range txs {
		css.WriteTx(tx)
	}

	shares := css.Export()
	assert.Condition(t, func() bool { return len(shares) == 1 })

	infoByte := shares[0][appconsts.NamespaceSize : appconsts.NamespaceSize+appconsts.ShareInfoBytes][0]

	isSequenceStart := true
	want, err := NewInfoByte(appconsts.ShareVersionZero, isSequenceStart)

	require.NoError(t, err)
	assert.Equal(t, byte(want), infoByte)
}

func TestContiguousCompactShareContainsInfoByte(t *testing.T) {
	css := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersionZero)
	txs := testfactory.GenerateRandomTxs(1, appconsts.ContinuationCompactShareContentSize*4)

	for _, tx := range txs {
		css.WriteTx(tx)
	}

	shares := css.Export()
	assert.Condition(t, func() bool { return len(shares) > 1 })

	infoByte := shares[1][appconsts.NamespaceSize : appconsts.NamespaceSize+appconsts.ShareInfoBytes][0]

	isSequenceStart := false
	want, err := NewInfoByte(appconsts.ShareVersionZero, isSequenceStart)

	require.NoError(t, err)
	assert.Equal(t, byte(want), infoByte)
}

func Test_parseCompactSharesErrors(t *testing.T) {
	type testCase struct {
		name      string
		rawShares [][]byte
	}

	txs := testfactory.GenerateRandomTxs(2, appconsts.ContinuationCompactShareContentSize*4)
	shares, _ := SplitTxs(txs)
	rawShares := ToBytes(shares)

	unsupportedShareVersion := 5
	infoByte, _ := NewInfoByte(uint8(unsupportedShareVersion), true)
	shareWithUnsupportedShareVersion := rawShares[0]
	shareWithUnsupportedShareVersion[appconsts.NamespaceSize] = byte(infoByte)

	testCases := []testCase{
		{
			"share with start indicator false",
			rawShares[1:], // set the first share to the second share which has the start indicator set to false
		},
		{
			"share with unsupported share version",
			[][]byte{shareWithUnsupportedShareVersion},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCompactShares(tt.rawShares, appconsts.SupportedShareVersions)
			assert.Error(t, err)
		})
	}
}
