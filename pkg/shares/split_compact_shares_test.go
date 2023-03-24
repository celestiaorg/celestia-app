package shares

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{transactions: []coretypes.Tx{bytes.Repeat([]byte{1}, 100)}, wantShareCount: 1},
		// Test with 1 byte over 1 share
		{transactions: []coretypes.Tx{bytes.Repeat([]byte{1}, rawTxSize(appconsts.FirstCompactShareContentSize+1))}, wantShareCount: 2},
		{transactions: []coretypes.Tx{generateTx(1)}, wantShareCount: 1},
		{transactions: []coretypes.Tx{generateTx(2)}, wantShareCount: 2},
		{transactions: []coretypes.Tx{generateTx(20)}, wantShareCount: 20},
	}
	for _, tc := range testCases {
		css := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersionZero)
		for _, transaction := range tc.transactions {
			err := css.WriteTx(transaction)
			require.NoError(t, err)
		}
		got := css.Count()
		if got != tc.wantShareCount {
			t.Errorf("count got %d want %d", got, tc.wantShareCount)
		}
	}
}

// generateTx generates a transaction that occupies exactly numShares number of
// shares.
func generateTx(numShares int) coretypes.Tx {
	if numShares == 0 {
		return coretypes.Tx{}
	}
	if numShares == 1 {
		return bytes.Repeat([]byte{1}, rawTxSize(appconsts.FirstCompactShareContentSize))
	}
	return bytes.Repeat([]byte{2}, rawTxSize(appconsts.FirstCompactShareContentSize+(numShares-1)*appconsts.ContinuationCompactShareContentSize))
}

func TestExport_write(t *testing.T) {
	type testCase struct {
		name       string
		want       []Share
		writeBytes [][]byte
	}

	// since we will import the raw share data, the inputs here do not matter
	builder := NewEmptyBuilder().ImportRawShare([]byte{
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
		0x1,                // info byte
		0x0, 0x0, 0x0, 0x1, // sequence len
		0x0, 0x0, 0x0, 17, // reserved bytes
		0xf, // data
	})

	builder.ZeroPadIfNecessary()
	oneShare, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	builder.ImportRawShare([]byte{
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
		0x1,                // info byte
		0x0, 0x0, 0x2, 0x0, // sequence len
		0x0, 0x0, 0x0, 17, // reserved bytes
		0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, // data
	})
	builder.ZeroPadIfNecessary()
	firstShare, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	builder.ImportRawShare([]byte{
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
		0x0,                // info byte
		0x0, 0x0, 0x0, 0x0, // reserved bytes
		0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, 0xf, // data
	})
	builder.ZeroPadIfNecessary()
	continuationShare, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	testCases := []testCase{
		{
			name: "empty",
			want: []Share{},
		},
		{
			name: "one share with small sequence len",
			want: []Share{
				*oneShare,
			},
			writeBytes: [][]byte{{0xf}},
		},
		{
			name: "two shares with big sequence len",
			want: []Share{
				*firstShare,
				*continuationShare,
			},
			writeBytes: [][]byte{bytes.Repeat([]byte{0xf}, 512)},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			css := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersionZero)
			for _, bytes := range tc.writeBytes {
				err := css.write(bytes)
				require.NoError(t, err)
			}
			got, _, err := css.Export(0)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			shares, _, err := css.Export(0)
			require.NoError(t, err)
			assert.Equal(t, got, shares)
			assert.Len(t, got, css.Count())
		})
	}
}

func TestWriteAndExportIdempotence(t *testing.T) {
	type testCase struct {
		name    string
		txs     []coretypes.Tx
		wantLen int
	}
	testCases := []testCase{
		{
			name:    "one tx that occupies exactly one share",
			txs:     []coretypes.Tx{generateTx(1)},
			wantLen: 1,
		},
		{
			name:    "one tx that occupies exactly two shares",
			txs:     []coretypes.Tx{generateTx(2)},
			wantLen: 2,
		},
		{
			name:    "one tx that occupies exactly three shares",
			txs:     []coretypes.Tx{generateTx(3)},
			wantLen: 3,
		},
		{
			name: "two txs that occupy exactly two shares",
			txs: []coretypes.Tx{
				bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.FirstCompactShareContentSize)),
				bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.ContinuationCompactShareContentSize)),
			},
			wantLen: 2,
		},
		{
			name: "three txs that occupy exactly three shares",
			txs: []coretypes.Tx{
				bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.FirstCompactShareContentSize)),
				bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.ContinuationCompactShareContentSize)),
				bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.ContinuationCompactShareContentSize)),
			},
			wantLen: 3,
		},
		{
			name: "four txs that occupy three full shares and one partial share",
			txs: []coretypes.Tx{
				bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.FirstCompactShareContentSize)),
				bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.ContinuationCompactShareContentSize)),
				bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.ContinuationCompactShareContentSize)),
				[]byte{0xf},
			},
			wantLen: 4,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			css := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersionZero)

			for _, tx := range tc.txs {
				err := css.WriteTx(tx)
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantLen, css.Count())
			shares, _, err := css.Export(0)
			require.NoError(t, err)
			assert.Equal(t, tc.wantLen, len(shares))
		})
	}
}

func TestExport(t *testing.T) {
	type testCase struct {
		name             string
		txs              []coretypes.Tx
		want             map[coretypes.TxKey]ShareRange
		shareRangeOffset int
	}

	txOne := coretypes.Tx{0x1}
	txTwo := coretypes.Tx(bytes.Repeat([]byte{2}, 600))
	txThree := coretypes.Tx(bytes.Repeat([]byte{3}, 1000))
	exactlyOneShare := coretypes.Tx(bytes.Repeat([]byte{4}, rawTxSize(appconsts.FirstCompactShareContentSize)))
	exactlyTwoShares := coretypes.Tx(bytes.Repeat([]byte{5}, rawTxSize(appconsts.FirstCompactShareContentSize+appconsts.ContinuationCompactShareContentSize)))

	testCases := []testCase{
		{
			name: "empty",
			txs:  []coretypes.Tx{},
			want: map[coretypes.TxKey]ShareRange{},
		},
		{
			name: "txOne occupies shares 0 to 0",
			txs: []coretypes.Tx{
				txOne,
			},
			want: map[coretypes.TxKey]ShareRange{
				txOne.Key(): {0, 0},
			},
		},
		{
			name: "txTwo occupies shares 0 to 1",
			txs: []coretypes.Tx{
				txTwo,
			},
			want: map[coretypes.TxKey]ShareRange{
				txTwo.Key(): {0, 1},
			},
		},
		{
			name: "txThree occupies shares 0 to 2",
			txs: []coretypes.Tx{
				txThree,
			},
			want: map[coretypes.TxKey]ShareRange{
				txThree.Key(): {0, 2},
			},
		},
		{
			name: "txOne occupies shares 0 to 0, txTwo occupies shares 0 to 1, txThree occupies shares 1 to 3",
			txs: []coretypes.Tx{
				txOne,
				txTwo,
				txThree,
			},
			want: map[coretypes.TxKey]ShareRange{
				txOne.Key():   {0, 0},
				txTwo.Key():   {0, 1},
				txThree.Key(): {1, 3},
			},
		},

		{
			name: "exactly one share occupies shares 0 to 0",
			txs: []coretypes.Tx{
				exactlyOneShare,
			},
			want: map[coretypes.TxKey]ShareRange{
				exactlyOneShare.Key(): {0, 0},
			},
		},
		{
			name: "exactly two shares occupies shares 0 to 1",
			txs: []coretypes.Tx{
				exactlyTwoShares,
			},
			want: map[coretypes.TxKey]ShareRange{
				exactlyTwoShares.Key(): {0, 1},
			},
		},
		{
			name: "two shares followed by one share",
			txs: []coretypes.Tx{
				exactlyTwoShares,
				exactlyOneShare,
			},
			want: map[coretypes.TxKey]ShareRange{
				exactlyTwoShares.Key(): {0, 1},
				exactlyOneShare.Key():  {2, 2},
			},
		},
		{
			name: "one share followed by two shares",
			txs: []coretypes.Tx{
				exactlyOneShare,
				exactlyTwoShares,
			},
			want: map[coretypes.TxKey]ShareRange{
				exactlyOneShare.Key():  {0, 0},
				exactlyTwoShares.Key(): {1, 2},
			},
		},
		{
			name: "one share followed by two shares offset by 10",
			txs: []coretypes.Tx{
				exactlyOneShare,
				exactlyTwoShares,
			},
			want: map[coretypes.TxKey]ShareRange{
				exactlyOneShare.Key():  {10, 10},
				exactlyTwoShares.Key(): {11, 12},
			},
			shareRangeOffset: 10,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			css := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersionZero)

			for _, tx := range tc.txs {
				err := css.WriteTx(tx)
				require.NoError(t, err)
			}

			_, got, err := css.Export(tc.shareRangeOffset)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestWriteAfterExport(t *testing.T) {
	a := bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.FirstCompactShareContentSize))
	b := bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.ContinuationCompactShareContentSize*2))
	c := bytes.Repeat([]byte{0xf}, rawTxSize(appconsts.ContinuationCompactShareContentSize))
	d := []byte{0xf}

	css := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersionZero)
	shares, _, err := css.Export(0)
	require.NoError(t, err)
	assert.Equal(t, 0, len(shares))

	err = css.WriteTx(a)
	require.NoError(t, err)

	shares, _, err = css.Export(0)
	require.NoError(t, err)
	assert.Equal(t, 1, len(shares))

	err = css.WriteTx(b)
	require.NoError(t, err)

	shares, _, err = css.Export(0)
	require.NoError(t, err)
	assert.Equal(t, 3, len(shares))

	err = css.WriteTx(c)
	require.NoError(t, err)

	shares, _, err = css.Export(0)
	require.NoError(t, err)
	assert.Equal(t, 4, len(shares))

	err = css.WriteTx(d)
	require.NoError(t, err)

	shares, _, err = css.Export(0)
	require.NoError(t, err)
	assert.Equal(t, 5, len(shares))

	shares, _, err = css.Export(0)
	require.NoError(t, err)
	assert.Equal(t, 5, len(shares))
}

// rawTxSize returns the raw tx size that can be used to construct a
// tx of desiredSize bytes. This function is useful in tests to account for
// the length delimiter that is prefixed to a tx.
func rawTxSize(desiredSize int) int {
	return desiredSize - DelimLen(uint64(desiredSize))
}
