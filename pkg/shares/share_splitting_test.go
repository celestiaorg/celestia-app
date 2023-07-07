package shares

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestSplitTxs_forTxShares(t *testing.T) {
	smallTransactionA := coretypes.Tx{0xa}
	smallTransactionB := coretypes.Tx{0xb}
	largeTransaction := bytes.Repeat([]byte{0xc}, 512)

	type testCase struct {
		name string
		txs  coretypes.Txs
		want []Share
	}
	testCases := []testCase{
		{
			name: "empty txs",
			txs:  coretypes.Txs{},
			want: []Share{},
		},
		{
			name: "one small tx",
			txs:  coretypes.Txs{smallTransactionA},
			want: []Share{
				padShare(Share{
					data: append(
						appns.TxNamespace.Bytes(),
						[]byte{
							0x1,                // info byte
							0x0, 0x0, 0x0, 0x2, // 1 byte (unit) + 1 byte (unit length) = 2 bytes sequence length
							0x0, 0x0, 0x0, 0x26, // reserved bytes
							0x1, // unit length of first transaction
							0xa, // data of first transaction
						}...,
					),
				},
				),
			},
		},
		{
			name: "two small txs",
			txs:  coretypes.Txs{smallTransactionA, smallTransactionB},
			want: []Share{
				padShare(Share{
					data: append(
						appns.TxNamespace.Bytes(),
						[]byte{
							0x1,                // info byte
							0x0, 0x0, 0x0, 0x4, // 2 bytes (first transaction) + 2 bytes (second transaction) = 4 bytes sequence length
							0x0, 0x0, 0x0, 0x26, // reserved bytes
							0x1, // unit length of first transaction
							0xa, // data of first transaction
							0x1, // unit length of second transaction
							0xb, // data of second transaction
						}...,
					),
				},
				),
			},
		},
		{
			name: "one large tx that spans two shares",
			txs:  coretypes.Txs{largeTransaction},
			want: []Share{
				fillShare(Share{
					data: append(
						appns.TxNamespace.Bytes(),
						[]byte{
							0x1,                // info byte
							0x0, 0x0, 0x2, 0x2, // 512 (unit) + 2 (unit length) = 514 sequence length
							0x0, 0x0, 0x0, 0x26, // reserved bytes
							128, 4, // unit length of transaction is 512
						}...,
					),
				},
					0xc, // data of transaction
				),
				padShare(Share{
					data: append(
						append(
							appns.TxNamespace.Bytes(),
							[]byte{
								0x0,                // info byte
								0x0, 0x0, 0x0, 0x0, // reserved bytes
							}...,
						),
						bytes.Repeat([]byte{0xc}, 40)..., // continuation data of transaction
					),
				},
				),
			},
		},
		{
			name: "one small tx then one large tx that spans two shares",
			txs:  coretypes.Txs{smallTransactionA, largeTransaction},
			want: []Share{
				fillShare(Share{
					data: append(
						appns.TxNamespace.Bytes(),
						[]byte{
							0x1,                // info byte
							0x0, 0x0, 0x2, 0x4, // 2 bytes (first transaction) + 514 bytes (second transaction) = 516 bytes sequence length
							0x0, 0x0, 0x0, 0x26, // reserved bytes
							1,      // unit length of first transaction
							0xa,    // data of first transaction
							128, 4, // unit length of second transaction is 512
						}...,
					),
				},
					0xc, // data of second transaction
				),
				padShare(Share{
					data: append(
						append(
							appns.TxNamespace.Bytes(),
							[]byte{
								0x0,                // info byte
								0x0, 0x0, 0x0, 0x0, // reserved bytes
							}...,
						),
						bytes.Repeat([]byte{0xc}, 42)..., // continuation data of second transaction
					),
				},
				),
			},
		},
		{
			name: "one large tx that spans two shares then one small tx",
			txs:  coretypes.Txs{largeTransaction, smallTransactionA},
			want: []Share{
				fillShare(Share{
					data: append(
						appns.TxNamespace.Bytes(),
						[]byte{
							0x1,                // info byte
							0x0, 0x0, 0x2, 0x4, // 514 bytes (first transaction) + 2 bytes (second transaction) = 516 bytes sequence length
							0x0, 0x0, 0x0, 0x26, // reserved bytes
							128, 4, // unit length of first transaction is 512
						}...,
					),
				},
					0xc, // data of first transaction
				),
				padShare(Share{
					data: append(
						appns.TxNamespace.Bytes(),
						[]byte{
							0x0,                 // info byte
							0x0, 0x0, 0x0, 0x4a, // reserved bytes
							0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, // continuation data of first transaction
							0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, // continuation data of first transaction
							0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, // continuation data of first transaction
							1,   // unit length of second transaction
							0xa, // data of second transaction
						}...,
					),
				},
				),
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got, _, _, err := SplitTxs(tt.txs)
			require.NoError(t, err)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitTxs()\n got %#v\n want %#v", got, tt.want)
			}
		})
	}
}

func TestSplitTxs(t *testing.T) {
	type testCase struct {
		name          string
		txs           coretypes.Txs
		wantTxShares  []Share
		wantPfbShares []Share
		wantMap       map[coretypes.TxKey]Range
	}

	smallTx := coretypes.Tx{0xa} // spans one share
	smallTxShares := []Share{
		padShare(Share{
			data: append(appns.TxNamespace.Bytes(),
				[]byte{
					0x1,                // info byte
					0x0, 0x0, 0x0, 0x2, // 1 byte (unit) + 1 byte (unit length) = 2 bytes sequence length
					0x0, 0x0, 0x0, 0x26, // reserved bytes
					0x1, // unit length of first transaction
					0xa, // data of first transaction
				}...,
			),
		},
		),
	}

	pfbTx, err := coretypes.MarshalIndexWrapper(coretypes.Tx{0xb}, 10) // spans one share
	require.NoError(t, err)
	pfbTxShares := []Share{
		padShare(Share{
			data: append(
				appns.PayForBlobNamespace.Bytes(),
				[]uint8{
					0x1,               // info byte
					0x0, 0x0, 0x0, 13, // 1 byte (unit) + 1 byte (unit length) = 2 bytes sequence length
					0x0, 0x0, 0x0, 0x26, // reserved bytes
					12,                                                               // unit length of first transaction
					0xa, 0x1, 0xb, 0x12, 0x1, 0xa, 0x1a, 0x4, 0x49, 0x4e, 0x44, 0x58, // data of first transaction
				}...,
			),
		},
		),
	}

	largeTx := coretypes.Tx(bytes.Repeat([]byte{0xc}, appconsts.ShareSize)) // spans two shares
	largeTxShares := []Share{
		fillShare(Share{
			data: append(appns.TxNamespace.Bytes(),
				[]uint8{
					0x1,                // info byte
					0x0, 0x0, 0x2, 0x2, // 512 (unit) + 2 (unit length) = 514 sequence length
					0x0, 0x0, 0x0, 0x26, // reserved bytes
					128, 4, // unit length of transaction is 512
				}...,
			),
		},
			0xc), // data of transaction
		padShare(Share{
			data: append(
				append(
					appns.TxNamespace.Bytes(),
					[]uint8{
						0x0,                // info byte
						0x0, 0x0, 0x0, 0x0, // reserved bytes
					}...,
				),
				bytes.Repeat([]byte{0xc}, 40)..., // continuation data of transaction
			),
		},
		),
	}

	testCases := []testCase{
		{
			name:          "empty",
			txs:           coretypes.Txs{},
			wantTxShares:  []Share{},
			wantPfbShares: []Share{},
			wantMap:       map[coretypes.TxKey]Range{},
		},
		{
			name:          "smallTx",
			txs:           coretypes.Txs{smallTx},
			wantTxShares:  smallTxShares,
			wantPfbShares: []Share{},
			wantMap: map[coretypes.TxKey]Range{
				smallTx.Key(): {0, 1},
			},
		},
		{
			name:          "largeTx",
			txs:           coretypes.Txs{largeTx},
			wantTxShares:  largeTxShares,
			wantPfbShares: []Share{},
			wantMap: map[coretypes.TxKey]Range{
				largeTx.Key(): {0, 2},
			},
		},
		{
			name:          "pfbTx",
			txs:           coretypes.Txs{pfbTx},
			wantTxShares:  []Share{},
			wantPfbShares: pfbTxShares,
			wantMap: map[coretypes.TxKey]Range{
				pfbTx.Key(): {0, 1},
			},
		},
		{
			name:          "largeTx then pfbTx",
			txs:           coretypes.Txs{largeTx, pfbTx},
			wantTxShares:  largeTxShares,
			wantPfbShares: pfbTxShares,
			wantMap: map[coretypes.TxKey]Range{
				largeTx.Key(): {0, 2},
				pfbTx.Key():   {2, 3},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			txShares, pfbTxShares, gotMap, err := SplitTxs(tc.txs)
			require.NoError(t, err)
			assert.Equal(t, tc.wantTxShares, txShares)
			assert.Equal(t, tc.wantPfbShares, pfbTxShares)
			assert.Equal(t, tc.wantMap, gotMap)
		})
	}
}

// padShare returns a share padded with trailing zeros.
func padShare(share Share) (paddedShare Share) {
	return fillShare(share, 0)
}

// fillShare returns a share filled with filler so that the share length
// is equal to appconsts.ShareSize.
func fillShare(share Share, filler byte) (paddedShare Share) {
	return Share{data: append(share.data, bytes.Repeat([]byte{filler}, appconsts.ShareSize-len(share.data))...)}
}

func Test_mergeMaps(t *testing.T) {
	type testCase struct {
		name   string
		mapOne map[coretypes.TxKey]Range
		mapTwo map[coretypes.TxKey]Range
		want   map[coretypes.TxKey]Range
	}
	testCases := []testCase{
		{
			name:   "empty maps",
			mapOne: map[coretypes.TxKey]Range{},
			mapTwo: map[coretypes.TxKey]Range{},
			want:   map[coretypes.TxKey]Range{},
		},
		{
			name: "merges maps with one key each",
			mapOne: map[coretypes.TxKey]Range{
				{0x1}: {0, 1},
			},
			mapTwo: map[coretypes.TxKey]Range{
				{0x2}: {2, 3},
			},
			want: map[coretypes.TxKey]Range{
				{0x1}: {0, 1},
				{0x2}: {2, 3},
			},
		},
		{
			name: "merges maps with multiple keys each",
			mapOne: map[coretypes.TxKey]Range{
				{0x1}: {0, 1},
				{0x2}: {2, 3},
			},
			mapTwo: map[coretypes.TxKey]Range{
				{0x3}: {3, 3},
				{0x4}: {4, 4},
			},
			want: map[coretypes.TxKey]Range{
				{0x1}: {0, 1},
				{0x2}: {2, 3},
				{0x3}: {3, 3},
				{0x4}: {4, 4},
			},
		},
		{
			name: "merges maps with a duplicate key and the second map's value takes precedence",
			mapOne: map[coretypes.TxKey]Range{
				{0x1}: {0, 0},
			},
			mapTwo: map[coretypes.TxKey]Range{
				{0x1}: {1, 1},
			},
			want: map[coretypes.TxKey]Range{
				{0x1}: {1, 1},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeMaps(tc.mapOne, tc.mapTwo)
			assert.Equal(t, tc.want, got)
		})
	}
}
