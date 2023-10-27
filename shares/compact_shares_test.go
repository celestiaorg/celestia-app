package shares

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestCompactShareSplitter(t *testing.T) {
	// note that this test is mainly for debugging purposes, the main round trip
	// tests occur in TestMerge and Test_processCompactShares
	css := NewCompactShareSplitter(TxNamespace, ShareVersionZero)
	txs := GenerateRandomTxs(33, 200)
	for _, tx := range txs {
		err := css.WriteTx(tx)
		require.NoError(t, err)
	}
	shares, err := css.Export()
	require.NoError(t, err)

	rawResTxs, err := parseCompactShares(shares, SupportedShareVersions)
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
	const exactTxShareSize = FirstCompactShareContentSize - 1

	type test struct {
		name    string
		txSize  int
		txCount int
	}

	// each test is ran twice, once using txSize as an exact size, and again
	// using it as a cap for randomly sized txs
	tests := []test{
		{"single small tx", ContinuationCompactShareContentSize / 8, 1},
		{"many small txs", ContinuationCompactShareContentSize / 8, 10},
		{"single big tx", ContinuationCompactShareContentSize * 4, 1},
		{"many big txs", ContinuationCompactShareContentSize * 4, 10},
		{"single exact size tx", exactTxShareSize, 1},
		{"many exact size txs", exactTxShareSize, 100},
	}

	for _, tc := range tests {
		tc := tc

		// run the tests with identically sized txs
		t.Run(fmt.Sprintf("%s idendically sized", tc.name), func(t *testing.T) {
			txs := GenerateRandomTxs(tc.txCount, tc.txSize)

			shares, _, _, err := SplitTxs(txs)
			require.NoError(t, err)

			parsedTxs, err := parseCompactShares(shares, SupportedShareVersions)
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
			txs := GenerateRandomlySizedTxs(tc.txCount, tc.txSize)

			txShares, _, _, err := SplitTxs(txs)
			require.NoError(t, err)
			parsedTxs, err := parseCompactShares(txShares, SupportedShareVersions)
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

func TestAllSplit(t *testing.T) {
	txs := GenerateRandomlySizedTxs(1000, 150)
	txShares, _, _, err := SplitTxs(txs)
	require.NoError(t, err)
	resTxs, err := ParseTxs(txShares)
	require.NoError(t, err)
	assert.Equal(t, resTxs, txs)
}

func TestParseRandomOutOfContextShares(t *testing.T) {
	txs := GenerateRandomlySizedTxs(1000, 150)
	txShares, _, _, err := SplitTxs(txs)
	require.NoError(t, err)

	for i := 0; i < 1000; i++ {
		start, length := GetRandomSubSlice(len(txShares))
		randomRange := NewRange(start, start+length)
		resTxs, err := ParseTxs(txShares[randomRange.Start:randomRange.End])
		require.NoError(t, err)
		assert.True(t, CheckSubArray(txs, resTxs))
	}
}

func TestParseOutOfContextSharesUsingShareRanges(t *testing.T) {
	txs := GenerateRandomlySizedTxs(1000, 150)
	txShares, _, shareRanges, err := SplitTxs(txs)
	require.NoError(t, err)

	for key, r := range shareRanges {
		resTxs, err := ParseTxs(txShares[r.Start:r.End])
		require.NoError(t, err)
		has := false
		for _, tx := range resTxs {
			if tx.Key() == key {
				has = true
				break
			}
		}
		assert.True(t, has)
	}
}

func TestCompactShareContainsInfoByte(t *testing.T) {
	css := NewCompactShareSplitter(TxNamespace, ShareVersionZero)
	txs := GenerateRandomTxs(1, ContinuationCompactShareContentSize/4)

	for _, tx := range txs {
		err := css.WriteTx(tx)
		require.NoError(t, err)
	}

	shares, err := css.Export()
	require.NoError(t, err)
	assert.Condition(t, func() bool { return len(shares) == 1 })

	infoByte := shares[0].data[NamespaceSize : NamespaceSize+ShareInfoBytes][0]

	isSequenceStart := true
	want, err := NewInfoByte(ShareVersionZero, isSequenceStart)

	require.NoError(t, err)
	assert.Equal(t, byte(want), infoByte)
}

func TestContiguousCompactShareContainsInfoByte(t *testing.T) {
	css := NewCompactShareSplitter(TxNamespace, ShareVersionZero)
	txs := GenerateRandomTxs(1, ContinuationCompactShareContentSize*4)

	for _, tx := range txs {
		err := css.WriteTx(tx)
		require.NoError(t, err)
	}

	shares, err := css.Export()
	require.NoError(t, err)
	assert.Condition(t, func() bool { return len(shares) > 1 })

	infoByte := shares[1].data[NamespaceSize : NamespaceSize+ShareInfoBytes][0]

	isSequenceStart := false
	want, err := NewInfoByte(ShareVersionZero, isSequenceStart)

	require.NoError(t, err)
	assert.Equal(t, byte(want), infoByte)
}

func Test_parseCompactSharesErrors(t *testing.T) {
	type testCase struct {
		name   string
		shares []Share
	}

	txs := GenerateRandomTxs(2, ContinuationCompactShareContentSize*4)
	txShares, _, _, err := SplitTxs(txs)
	require.NoError(t, err)
	rawShares := ToBytes(txShares)

	unsupportedShareVersion := 5
	infoByte, _ := NewInfoByte(uint8(unsupportedShareVersion), true)
	shareWithUnsupportedShareVersionBytes := rawShares[0]
	shareWithUnsupportedShareVersionBytes[NamespaceSize] = byte(infoByte)

	shareWithUnsupportedShareVersion, err := NewShare(shareWithUnsupportedShareVersionBytes)
	if err != nil {
		t.Fatal(err)
	}

	testCases := []testCase{
		{
			"share with unsupported share version",
			[]Share{*shareWithUnsupportedShareVersion},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCompactShares(tt.shares, SupportedShareVersions)
			assert.Error(t, err)
		})
	}
}
