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
					0x1,                // info byte
					0x2, 0x0, 0x0, 0x0, // 1 byte (unit)  + 1 byte (unit length) = 2 bytes message length
					14,  // reserved byte
					0x1, // unit length of first transaction
					0xa, // data of first transaction
				}, bytes.Repeat([]byte{0}, 240)...), // padding
			},
		},
		{
			name: "two small txs",
			txs:  coretypes.Txs{coretypes.Tx{0xa}, coretypes.Tx{0xb}},
			want: [][]uint8{
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,                // info byte
					0x4, 0x0, 0x0, 0x0, // 2 bytes (first transaction) + 2 bytes (second transaction) = 4 bytes message length
					14,  // reserved byte
					0x1, // unit length of first transaction
					0xa, // data of first transaction
					0x1, // unit length of second transaction
					0xb, // data of second transaction
				}, bytes.Repeat([]byte{0}, 238)...), // padding
			},
		},
		{
			name: "one large tx that spans two shares",
			txs:  coretypes.Txs{bytes.Repeat([]byte{0xC}, 241)},
			want: [][]uint8{
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,          // info byte
					243, 1, 0, 0, // 241 (unit) + 2 (unit length) = 243 message length
					14,     // reserved byte
					241, 1, // unit length of first transaction is 241
				}, bytes.Repeat([]byte{0xc}, 240)...), // data of first transaction
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
			txs:  coretypes.Txs{coretypes.Tx{0xd}, bytes.Repeat([]byte{0xe}, 241)},
			want: [][]uint8{
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,          // info byte
					245, 1, 0, 0, // 2 bytes (first transaction) + 243 bytes (second transaction) = 245 bytes message length
					14,     // reserved byte
					1,      // unit length of first transaction
					0xd,    // data of first transaction
					241, 1, // unit length of second transaction is 241
				}, bytes.Repeat([]byte{0xe}, 238)...), // data of first transaction
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x0,           // info byte
					0x0,           // reserved byte
					0xe, 0xe, 0xe, // continuation data of second transaction
				}, bytes.Repeat([]byte{0x0}, 243)...), // padding
			},
		},
		{
			name: "one large tx that spans two shares then one small tx",
			txs:  coretypes.Txs{bytes.Repeat([]byte{0xe}, 241), coretypes.Tx{0xd}},
			want: [][]uint8{
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x1,          // info byte
					245, 1, 0, 0, // 243 bytes (first transaction) + 2 bytes (second transaction) = 245 bytes message length
					14,     // reserved byte
					241, 1, // unit length of first transaction is 241
				}, bytes.Repeat([]byte{0xe}, 240)...), // data of first transaction
				append([]uint8{
					0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, // namespace id
					0x0, // info byte
					11,  // reserved byte
					0xe, // continuation data of first transaction
					1,   // unit length of second transaction
					0xd, // data of second transaction
				}, bytes.Repeat([]byte{0x0}, 243)...), // padding
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
} //
