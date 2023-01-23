package shares

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
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
				padShare([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,                // info byte
					0x0, 0x0, 0x0, 0x2, // 1 byte (unit) + 1 byte (unit length) = 2 bytes sequence length
					0x0, 0x0, 0x0, 17, // reserved bytes
					0x1, // unit length of first transaction
					0xa, // data of first transaction
				}),
			},
		},
		{
			name: "two small txs",
			txs:  coretypes.Txs{smallTransactionA, smallTransactionB},
			want: []Share{
				padShare([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,                // info byte
					0x0, 0x0, 0x0, 0x4, // 2 bytes (first transaction) + 2 bytes (second transaction) = 4 bytes sequence length
					0x0, 0x0, 0x0, 17, // reserved bytes
					0x1, // unit length of first transaction
					0xa, // data of first transaction
					0x1, // unit length of second transaction
					0xb, // data of second transaction
				}),
			},
		},
		{
			name: "one large tx that spans two shares",
			txs:  coretypes.Txs{largeTransaction},
			want: []Share{
				fillShare([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,                // info byte
					0x0, 0x0, 0x2, 0x2, // 512 (unit) + 2 (unit length) = 514 sequence length
					0x0, 0x0, 0x0, 17, // reserved bytes
					128, 4, // unit length of transaction is 512
				}, 0xc), // data of transaction
				padShare(append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x0,                // info byte
					0x0, 0x0, 0x0, 0x0, // reserved bytes
				}, bytes.Repeat([]byte{0xc}, 19)..., // continuation data of transaction
				)),
			},
		},
		{
			name: "one small tx then one large tx that spans two shares",
			txs:  coretypes.Txs{smallTransactionA, largeTransaction},
			want: []Share{
				fillShare([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,                // info byte
					0x0, 0x0, 0x2, 0x4, // 2 bytes (first transaction) + 514 bytes (second transaction) = 516 bytes sequence length
					0x0, 0x0, 0x0, 17, // reserved bytes
					1,      // unit length of first transaction
					0xa,    // data of first transaction
					128, 4, // unit length of second transaction is 512
				}, 0xc), // data of second transaction
				padShare(
					append([]uint8{
						0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
						0x0,                // info byte
						0x0, 0x0, 0x0, 0x0, // reserved bytes
					}, bytes.Repeat([]byte{0xc}, 21)...), // continuation data of second transaction
				),
			},
		},
		{
			name: "one large tx that spans two shares then one small tx",
			txs:  coretypes.Txs{largeTransaction, smallTransactionA},
			want: []Share{
				fillShare([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,                // info byte
					0x0, 0x0, 0x2, 0x4, // 514 bytes (first transaction) + 2 bytes (second transaction) = 516 bytes sequence length
					0x0, 0x0, 0x0, 17, // reserved bytes
					128, 4, // unit length of first transaction is 512
				}, 0xc), // data of first transaction
				padShare([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x0,               // info byte
					0x0, 0x0, 0x0, 32, // reserved bytes
					0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, 0xc, // continuation data of first transaction
					1,   // unit length of second transaction
					0xa, // data of second transaction
				}),
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got, _, _ := SplitTxs(tt.txs)
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
		wantMap       map[coretypes.TxKey]ShareRange
	}

	// smallTx := coretypes.Tx{0xa}
	pfbTx, err := coretypes.MarshalIndexWrapper(coretypes.Tx{0xb}, 10)
	assert.NoError(t, err)
	largeTx := coretypes.Tx(bytes.Repeat([]byte{0xc}, subtractDelimLen(appconsts.FirstCompactShareContentSize)))

	testCases := []testCase{
		// {
		// 	name:          "empty",
		// 	txs:           coretypes.Txs{},
		// 	wantTxShares:  []Share{},
		// 	wantPfbShares: []Share{},
		// 	wantMap:       map[coretypes.TxKey]ShareRange{},
		// },
		// {
		// 	name: "smallTx",
		// 	txs:  coretypes.Txs{smallTx},
		// 	wantTxShares: []Share{
		// 		padShare([]uint8{
		// 			0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
		// 			0x1,                // info byte
		// 			0x0, 0x0, 0x0, 0x2, // 1 byte (unit) + 1 byte (unit length) = 2 bytes sequence length
		// 			0x0, 0x0, 0x0, 17, // reserved bytes
		// 			0x1, // unit length of first transaction
		// 			0xa, // data of first transaction
		// 		}),
		// 	},
		// 	wantPfbShares: []Share{},
		// 	wantMap: map[coretypes.TxKey]ShareRange{
		// 		smallTx.Key(): {0, 0},
		// 	},
		// },
		// {
		// 	name:         "pfbTx",
		// 	txs:          coretypes.Txs{pfbTx},
		// 	wantTxShares: []Share{},
		// 	wantPfbShares: []Share{
		// 		padShare([]uint8{
		// 			0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x4, // namespace id
		// 			0x1,               // info byte
		// 			0x0, 0x0, 0x0, 13, // 1 byte (unit) + 1 byte (unit length) = 2 bytes sequence length
		// 			0x0, 0x0, 0x0, 17, // reserved bytes
		// 			12,                                                               // unit length of first transaction
		// 			0xa, 0x1, 0xb, 0x12, 0x1, 0xa, 0x1a, 0x4, 0x49, 0x4e, 0x44, 0x58, // data of first transaction
		// 		}),
		// 	},
		// 	wantMap: map[coretypes.TxKey]ShareRange{
		// 		pfbTx.Key(): {0, 0},
		// 	},
		// },
		{
			name: "largeTx + pfbTx",
			txs:  coretypes.Txs{largeTx, pfbTx},
			wantTxShares: []Share{
				fillShare([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,                // info byte
					0x0, 0x0, 0x1, 239, // sequence length
					0x0, 0x0, 0x0, 17, // reserved bytes
					0xed, 0x3, // unit length of transaction
				}, 0xc), // data of transaction
			},
			wantPfbShares: []Share{
				padShare([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x4, // namespace id
					0x1,               // info byte
					0x0, 0x0, 0x0, 13, // 1 byte (unit) + 1 byte (unit length) = 2 bytes sequence length
					0x0, 0x0, 0x0, 17, // reserved bytes
					12,                                                               // unit length of first transaction
					0xa, 0x1, 0xb, 0x12, 0x1, 0xa, 0x1a, 0x4, 0x49, 0x4e, 0x44, 0x58, // data of first transaction
				}),
			},
			wantMap: map[coretypes.TxKey]ShareRange{
				// TODO this test doesn't behave as expected
				largeTx.Key(): {0, 0},
				pfbTx.Key():   {1, 1},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fmt.Printf("first txKey: %v", tc.txs[0].Key())
			fmt.Printf("second txKey: %v", tc.txs[1].Key())
			txShares, pfbTxShares, gotMap := SplitTxs(tc.txs)
			assert.Equal(t, tc.wantTxShares, txShares)
			assert.Equal(t, tc.wantPfbShares, pfbTxShares)
			assert.Equal(t, tc.wantMap, gotMap)
		})
	}
}

// padShare returns a share padded with trailing zeros.
func padShare(share []byte) (paddedShare []byte) {
	return fillShare(share, 0)
}

// fillShare returns a share filled with filler so that the share length
// is equal to appconsts.ShareSize.
func fillShare(share []byte, filler byte) (paddedShare []byte) {
	return append(share, bytes.Repeat([]byte{filler}, appconsts.ShareSize-len(share))...)
}
