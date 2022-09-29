package shares

import (
	"bytes"
	"reflect"
	"testing"

	coretypes "github.com/tendermint/tendermint/types"
)

func TestSplitTxs(t *testing.T) {
	type testCase struct {
		name string
		txs  coretypes.Txs
		want [][]byte
	}
	testCases := []testCase{
		{
			name: "empty txs",
			txs:  coretypes.Txs{},
			want: [][]byte{},
		},
		{
			name: "one small tx",
			txs:  coretypes.Txs{coretypes.Tx{0xa}},
			want: [][]uint8{
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1, // info byte
					0x0, // reserved byte
					0x1, // unit length of first transaction
					0xa, // data of first transaction
				}, bytes.Repeat([]byte{0}, 244)...), // padding
			},
		},
		{
			name: "two small txs",
			txs:  coretypes.Txs{coretypes.Tx{0xa}, coretypes.Tx{0xb}},
			want: [][]uint8{
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1, // info byte
					0x0, // reserved byte
					0x1, // unit length of first transaction
					0xa, // data of first transaction
					0x1, // unit length of second transaction
					0xb, // data of second transaction
				}, bytes.Repeat([]byte{0}, 242)...), // padding
			},
		},
		{
			name: "one large tx that spans two shares",
			txs:  coretypes.Txs{bytes.Repeat([]byte{0xC}, 245)},
			want: [][]uint8{
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,    // info byte
					0x0,    // BUG reserved byte should be non-zero see https://github.com/celestiaorg/celestia-app/issues/802
					245, 1, // unit length of first transaction is 245
				}, bytes.Repeat([]byte{0xc}, 244)...), // data of first transaction
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x0, // info byte
					0x0, // reserved byte
					0xc, // continuation data of first transaction
				}, bytes.Repeat([]byte{0x0}, 245)...), // padding
			},
		},
		{
			name: "one small tx then one large tx that spans two shares",
			txs:  coretypes.Txs{coretypes.Tx{0xd}, bytes.Repeat([]byte{0xe}, 243)},
			want: [][]uint8{
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,    // info byte
					0x0,    // BUG reserved byte should be non-zero see https://github.com/celestiaorg/celestia-app/issues/802
					1,      // unit length of first transaction
					0xd,    // data of first transaction
					243, 1, // unit length of second transaction is 243
				}, bytes.Repeat([]byte{0xe}, 242)...), // data of first transaction
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x0, // info byte
					0x0, // reserved byte
					0xe, // continuation data of second transaction
				}, bytes.Repeat([]byte{0x0}, 245)...), // padding
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitTxs(tt.txs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitTxs(%#v) got %#v, want %#v", tt.txs, got, tt.want)
			}
		})
	}
}
