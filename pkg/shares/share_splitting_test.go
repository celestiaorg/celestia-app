package shares

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestSplitTxs(t *testing.T) {
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
			got := SplitTxs(tt.txs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitTxs()\n got %#v\n want %#v", got, tt.want)
			}
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
