package shares

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
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
		{transactions: []coretypes.Tx{bytes.Repeat([]byte{1}, RawTxSize(appconsts.FirstCompactShareContentSize+1))}, wantShareCount: 2},
		{transactions: []coretypes.Tx{generateTx(1)}, wantShareCount: 1},
		{transactions: []coretypes.Tx{generateTx(2)}, wantShareCount: 2},
		{transactions: []coretypes.Tx{generateTx(20)}, wantShareCount: 20},
	}
	for _, tc := range testCases {
		css := NewCompactShareSplitter(appns.TxNamespace, appconsts.ShareVersionZero)
		for _, transaction := range tc.transactions {
			err := css.WriteTx(transaction)
			require.NoError(t, err)
		}
		got := css.Count()
		if got != tc.wantShareCount {
			t.Errorf("count got %d want %d", got, tc.wantShareCount)
		}
	}

	css := NewCompactShareSplitter(namespace.TxNamespace, appconsts.ShareVersionZero)
	assert.Equal(t, 0, css.Count())
}

// generateTx generates a transaction that occupies exactly numShares number of
// shares.
func generateTx(numShares int) coretypes.Tx {
	if numShares == 0 {
		return coretypes.Tx{}
	}
	if numShares == 1 {
		return bytes.Repeat([]byte{1}, RawTxSize(appconsts.FirstCompactShareContentSize))
	}
	return bytes.Repeat([]byte{2}, RawTxSize(appconsts.FirstCompactShareContentSize+(numShares-1)*appconsts.ContinuationCompactShareContentSize))
}

func TestExport_write(t *testing.T) {
	type testCase struct {
		name       string
		want       []Share
		writeBytes [][]byte
	}

	oneShare, _ := zeroPadIfNecessary(
		append(
			appns.TxNamespace.Bytes(),
			[]byte{
				0x1,                // info byte
				0x0, 0x0, 0x0, 0x1, // sequence len
				0x0, 0x0, 0x0, 0x26, // reserved bytes
				0xf, // data
			}...,
		),
		appconsts.ShareSize)

	firstShare := fillShare(Share{data: append(
		appns.TxNamespace.Bytes(),
		[]byte{
			0x1,                // info byte
			0x0, 0x0, 0x2, 0x0, // sequence len
			0x0, 0x0, 0x0, 0x26, // reserved bytes
		}...,
	)}, 0xf)

	continuationShare, _ := zeroPadIfNecessary(
		append(
			appns.TxNamespace.Bytes(),
			append(
				[]byte{
					0x0,                // info byte
					0x0, 0x0, 0x0, 0x0, // reserved bytes
				}, bytes.Repeat([]byte{0xf}, appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.SequenceLenBytes+appconsts.CompactShareReservedBytes)..., // data
			)...,
		),
		appconsts.ShareSize)

	testCases := []testCase{
		{
			name: "empty",
			want: []Share{},
		},
		{
			name: "one share with small sequence len",
			want: []Share{
				{data: oneShare},
			},
			writeBytes: [][]byte{{0xf}},
		},
		{
			name: "two shares with big sequence len",
			want: []Share{
				firstShare,
				{data: continuationShare},
			},
			writeBytes: [][]byte{bytes.Repeat([]byte{0xf}, 512)},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			css := NewCompactShareSplitter(appns.TxNamespace, appconsts.ShareVersionZero)
			for _, bytes := range tc.writeBytes {
				err := css.write(bytes)
				require.NoError(t, err)
			}
			got, err := css.Export()
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			shares, err := css.Export()
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
				bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.FirstCompactShareContentSize)),
				bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.ContinuationCompactShareContentSize)),
			},
			wantLen: 2,
		},
		{
			name: "three txs that occupy exactly three shares",
			txs: []coretypes.Tx{
				bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.FirstCompactShareContentSize)),
				bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.ContinuationCompactShareContentSize)),
				bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.ContinuationCompactShareContentSize)),
			},
			wantLen: 3,
		},
		{
			name: "four txs that occupy three full shares and one partial share",
			txs: []coretypes.Tx{
				bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.FirstCompactShareContentSize)),
				bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.ContinuationCompactShareContentSize)),
				bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.ContinuationCompactShareContentSize)),
				[]byte{0xf},
			},
			wantLen: 4,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			css := NewCompactShareSplitter(appns.TxNamespace, appconsts.ShareVersionZero)

			for _, tx := range tc.txs {
				err := css.WriteTx(tx)
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantLen, css.Count())
			shares, err := css.Export()
			require.NoError(t, err)
			assert.Equal(t, tc.wantLen, len(shares))
		})
	}
}

func TestExport(t *testing.T) {
	type testCase struct {
		name             string
		txs              []coretypes.Tx
		want             map[coretypes.TxKey]Range
		shareRangeOffset int
	}

	txOne := coretypes.Tx{0x1}
	txTwo := coretypes.Tx(bytes.Repeat([]byte{2}, 600))
	txThree := coretypes.Tx(bytes.Repeat([]byte{3}, 1000))
	exactlyOneShare := coretypes.Tx(bytes.Repeat([]byte{4}, RawTxSize(appconsts.FirstCompactShareContentSize)))
	exactlyTwoShares := coretypes.Tx(bytes.Repeat([]byte{5}, RawTxSize(appconsts.FirstCompactShareContentSize+appconsts.ContinuationCompactShareContentSize)))

	testCases := []testCase{
		{
			name: "empty",
			txs:  []coretypes.Tx{},
			want: map[coretypes.TxKey]Range{},
		},
		{
			name: "txOne occupies shares 0 to 0",
			txs: []coretypes.Tx{
				txOne,
			},
			want: map[coretypes.TxKey]Range{
				txOne.Key(): {0, 1},
			},
		},
		{
			name: "txTwo occupies shares 0 to 1",
			txs: []coretypes.Tx{
				txTwo,
			},
			want: map[coretypes.TxKey]Range{
				txTwo.Key(): {0, 2},
			},
		},
		{
			name: "txThree occupies shares 0 to 2",
			txs: []coretypes.Tx{
				txThree,
			},
			want: map[coretypes.TxKey]Range{
				txThree.Key(): {0, 3},
			},
		},
		{
			name: "txOne occupies shares 0 to 0, txTwo occupies shares 0 to 1, txThree occupies shares 1 to 3",
			txs: []coretypes.Tx{
				txOne,
				txTwo,
				txThree,
			},
			want: map[coretypes.TxKey]Range{
				txOne.Key():   {0, 1},
				txTwo.Key():   {0, 2},
				txThree.Key(): {1, 4},
			},
		},

		{
			name: "exactly one share occupies shares 0 to 0",
			txs: []coretypes.Tx{
				exactlyOneShare,
			},
			want: map[coretypes.TxKey]Range{
				exactlyOneShare.Key(): {0, 1},
			},
		},
		{
			name: "exactly two shares occupies shares 0 to 1",
			txs: []coretypes.Tx{
				exactlyTwoShares,
			},
			want: map[coretypes.TxKey]Range{
				exactlyTwoShares.Key(): {0, 2},
			},
		},
		{
			name: "two shares followed by one share",
			txs: []coretypes.Tx{
				exactlyTwoShares,
				exactlyOneShare,
			},
			want: map[coretypes.TxKey]Range{
				exactlyTwoShares.Key(): {0, 2},
				exactlyOneShare.Key():  {2, 3},
			},
		},
		{
			name: "one share followed by two shares",
			txs: []coretypes.Tx{
				exactlyOneShare,
				exactlyTwoShares,
			},
			want: map[coretypes.TxKey]Range{
				exactlyOneShare.Key():  {0, 1},
				exactlyTwoShares.Key(): {1, 3},
			},
		},
		{
			name: "one share followed by two shares offset by 10",
			txs: []coretypes.Tx{
				exactlyOneShare,
				exactlyTwoShares,
			},
			want: map[coretypes.TxKey]Range{
				exactlyOneShare.Key():  {10, 11},
				exactlyTwoShares.Key(): {11, 13},
			},
			shareRangeOffset: 10,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			css := NewCompactShareSplitter(appns.TxNamespace, appconsts.ShareVersionZero)

			for _, tx := range tc.txs {
				err := css.WriteTx(tx)
				require.NoError(t, err)
			}

			got := css.ShareRanges(tc.shareRangeOffset)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestWriteAfterExport(t *testing.T) {
	a := bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.FirstCompactShareContentSize))
	b := bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.ContinuationCompactShareContentSize*2))
	c := bytes.Repeat([]byte{0xf}, RawTxSize(appconsts.ContinuationCompactShareContentSize))
	d := []byte{0xf}

	css := NewCompactShareSplitter(appns.TxNamespace, appconsts.ShareVersionZero)
	shares, err := css.Export()
	require.NoError(t, err)
	assert.Equal(t, 0, len(shares))

	err = css.WriteTx(a)
	require.NoError(t, err)

	shares, err = css.Export()
	require.NoError(t, err)
	assert.Equal(t, 1, len(shares))

	err = css.WriteTx(b)
	require.NoError(t, err)

	shares, err = css.Export()
	require.NoError(t, err)
	assert.Equal(t, 3, len(shares))

	err = css.WriteTx(c)
	require.NoError(t, err)

	shares, err = css.Export()
	require.NoError(t, err)
	assert.Equal(t, 4, len(shares))

	err = css.WriteTx(d)
	require.NoError(t, err)

	shares, err = css.Export()
	require.NoError(t, err)
	assert.Equal(t, 5, len(shares))

	shares, err = css.Export()
	require.NoError(t, err)
	assert.Equal(t, 5, len(shares))
}
