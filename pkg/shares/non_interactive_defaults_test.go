package shares

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tendermint/tendermint/pkg/consts"
)

func TestMsgSharesUsedNIDefaults(t *testing.T) {
	type test struct {
		cursor, squareSize, expected int
		msgLens                      []int
		indexes                      []uint32
	}
	tests := []test{
		{2, 4, 1, []int{1}, []uint32{2}},
		{2, 2, 1, []int{1}, []uint32{2}},
		{3, 4, 8, []int{3, 3}, []uint32{4, 8}},
		{0, 8, 8, []int{8}, []uint32{0}},
		{0, 8, 7, []int{7}, []uint32{0}},
		{0, 8, 7, []int{3, 3}, []uint32{0, 4}},
		{1, 8, 8, []int{3, 3}, []uint32{2, 6}}, //
		{1, 8, 32, []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}},
		{3, 8, 16, []int{5, 7}, []uint32{4, 12}},
		{0, 8, 29, []int{5, 5, 5, 5}, []uint32{0, 8, 16, 24}},
		{0, 8, 10, []int{10}, []uint32{0}},
		{0, 8, 26, []int{10, 10}, []uint32{0, 16}},
		{1, 8, 33, []int{10, 10}, []uint32{8, 24}},
		{2, 8, 32, []int{10, 10}, []uint32{8, 24}},
		{0, 8, 55, []int{21, 31}, []uint32{0, 24}},
		{0, 8, 128, []int{64, 64}, []uint32{0, 64}},
		{0, consts.MaxSquareSize, 1000, []int{1000}, []uint32{0}},
		{0, consts.MaxSquareSize, consts.MaxSquareSize + 1, []int{consts.MaxSquareSize + 1}, []uint32{0}},
		{1, consts.MaxSquareSize, (consts.MaxSquareSize * 4) - 1, []int{consts.MaxSquareSize, consts.MaxSquareSize, consts.MaxSquareSize}, []uint32{128, 256, 384}},
		{1024, consts.MaxSquareSize, 32, []int{32}, []uint32{1024}},
	}
	for i, tt := range tests {
		res, indexes := MsgSharesUsedNIDefaults(tt.cursor, tt.squareSize, tt.msgLens...)
		test := fmt.Sprintf("test %d: cursor %d, squareSize %d", i, tt.cursor, tt.squareSize)
		assert.Equal(t, tt.expected, res, test)
		assert.Equal(t, tt.indexes, indexes, test)
	}
}

func TestFitsInSquare(t *testing.T) {
	type test struct {
		name  string
		msgs  []int
		start int
		size  int
		fits  bool
	}
	tests := []test{
		{
			name:  "1 msgs size 2 shares (2 msg shares, 2 compact, size 4)",
			msgs:  []int{2},
			start: 2,
			size:  4,
			fits:  true,
		},
		{
			name:  "10 msgs size 10 shares (100 msg shares, 0 compact, size 4)",
			msgs:  []int{10, 10, 10, 10, 10, 10, 10, 10, 10, 10},
			start: 0,
			size:  4,
			fits:  false,
		},
		{
			name:  "15 msgs size 1 share (15 msg shares, 0 compact, size 4)",
			msgs:  []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			start: 0,
			size:  4,
			fits:  true,
		},
		{
			name:  "15 msgs size 1 share starting at share 2 (15 msg shares, 2 compact, size 4)",
			msgs:  []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			start: 2,
			size:  4,
			fits:  false,
		},
		{
			name:  "8 msgs of various sizes (63 msg shares, 1 compact share, size 8)",
			msgs:  []int{3, 9, 3, 7, 8, 3, 7, 8},
			start: 1,
			size:  8,
			fits:  true,
		},
		{
			name:  "8 msgs of various sizes (63 msg shares, 6 compact, size 8)",
			msgs:  []int{3, 9, 3, 7, 8, 3, 7, 8},
			start: 6,
			size:  8,
			fits:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, _ := FitsInSquare(tt.start, tt.size, tt.msgs...)
			assert.Equal(t, tt.fits, res)
		})
	}
}

func TestNextAlignedPowerOfTwo(t *testing.T) {
	type test struct {
		name                       string
		cursor, msgLen, squareSize int
		expectedIndex              int
		fits                       bool
	}
	tests := []test{
		{
			name:          "whole row msgLen 4",
			cursor:        0,
			msgLen:        4,
			squareSize:    4,
			fits:          true,
			expectedIndex: 0,
		},
		{
			name:          "half row msgLen 2 cursor 1",
			cursor:        1,
			msgLen:        2,
			squareSize:    4,
			fits:          true,
			expectedIndex: 2,
		},
		{
			name:          "half row msgLen 2 cursor 2",
			cursor:        2,
			msgLen:        2,
			squareSize:    4,
			fits:          true,
			expectedIndex: 2,
		},
		{
			name:          "half row msgLen 4 cursor 3",
			cursor:        3,
			msgLen:        4,
			squareSize:    8,
			fits:          true,
			expectedIndex: 4,
		},
		{
			name:          "msgLen 5 cursor 3 size 8",
			cursor:        3,
			msgLen:        5,
			squareSize:    8,
			fits:          false,
			expectedIndex: 4,
		},
		{
			name:          "msgLen 2 cursor 3 square size 8",
			cursor:        3,
			msgLen:        2,
			squareSize:    8,
			fits:          true,
			expectedIndex: 4,
		},
		{
			name:          "cursor 3 msgLen 5 size 8",
			cursor:        3,
			msgLen:        5,
			squareSize:    8,
			fits:          false,
			expectedIndex: 4,
		},
		{
			name:          "msglen 12 cursor 1 size 16",
			cursor:        1,
			msgLen:        12,
			squareSize:    16,
			fits:          false,
			expectedIndex: 8,
		},
		{
			name:          "edge case where there are many messages with a single size",
			cursor:        10291,
			msgLen:        1,
			squareSize:    128,
			fits:          true,
			expectedIndex: 10291,
		},
		{
			name:          "second row msgLen 2 cursor 11 square size 8",
			cursor:        11,
			msgLen:        2,
			squareSize:    8,
			fits:          true,
			expectedIndex: 12,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, fits := NextAlignedPowerOfTwo(tt.cursor, tt.msgLen, tt.squareSize)
			assert.Equal(t, tt.fits, fits)
			assert.Equal(t, tt.expectedIndex, res)
		})
	}
}

func Test_roundUpBy(t *testing.T) {
	type test struct {
		cursor, v     int
		expectedIndex int
	}
	tests := []test{
		{
			cursor:        1,
			v:             2,
			expectedIndex: 2,
		},
		{
			cursor:        2,
			v:             2,
			expectedIndex: 2,
		},
		{
			cursor:        0,
			v:             2,
			expectedIndex: 0,
		},
		{
			cursor:        5,
			v:             2,
			expectedIndex: 6,
		},
		{
			cursor:        8,
			v:             16,
			expectedIndex: 16,
		},
		{
			cursor:        33,
			v:             1,
			expectedIndex: 33,
		},
		{
			cursor:        32,
			v:             16,
			expectedIndex: 32,
		},
		{
			cursor:        33,
			v:             16,
			expectedIndex: 48,
		},
	}
	for i, tt := range tests {
		t.Run(
			fmt.Sprintf(
				"test %d: %d cursor %d v %d expectedIndex",
				i,
				tt.cursor,
				tt.v,
				tt.expectedIndex,
			),
			func(t *testing.T) {
				res := roundUpBy(tt.cursor, tt.v)
				assert.Equal(t, tt.expectedIndex, res)
			})
	}
}
