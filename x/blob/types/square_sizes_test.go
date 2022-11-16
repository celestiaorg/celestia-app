package types

import (
	"reflect"
	"testing"
)

func TestAllSquareSizes(t *testing.T) {
	type testCase struct {
		msgSize int
		want    []uint64
	}

	tests := []testCase{
		{0, []uint64{2, 4, 8, 16, 32, 64, 128}}, // should this function return an error for a message size of 0?
		{1, []uint64{2, 4, 8, 16, 32, 64, 128}},
		{2, []uint64{2, 4, 8, 16, 32, 64, 128}},
		{4, []uint64{2, 4, 8, 16, 32, 64, 128}},
		{8, []uint64{2, 4, 8, 16, 32, 64, 128}},
		// A square size of 2 has 4 shares. 4 shares * 512 bytes per share =
		// 2048 bytes. So a square size of 2 is too small to fit a message of
		// size 2048 bytes.
		{2048, []uint64{4, 8, 16, 32, 64, 128}},
		// A square size of 4 has 16 shares. 16 shares * 512 bytes per share =
		// 8192 bytes. So a square size of 4 is too small to fit a message of
		// size 8192 bytes.
		{8192, []uint64{8, 16, 32, 64, 128}},
		// A square size of 8 has 64 shares. 64 shares * 512 bytes per share =
		// 32768 bytes. So a square size of 4 is too small to fit a message of
		// size 32768 bytes.
		{32768, []uint64{16, 32, 64, 128}},
		// A square size of 128 has 16384 shares. 16384 shares * 512 bytes per share =
		// 8388608 bytes. So a square size of 128 is too small to fit a message of
		// size 16384 bytes.
		{8388608, []uint64{}},
	}

	for _, test := range tests {
		got := AllSquareSizes(test.msgSize)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("AllSquareSizes(%d) got %v, want %v", test.msgSize, got, test.want)
		}
	}
}
