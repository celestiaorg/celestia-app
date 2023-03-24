package shares

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
)

func TestBlobSharesUsedNonInteractiveDefaults(t *testing.T) {
	type test struct {
		cursor, squareSize, expected int
		blobLens                     []int
		indexes                      []uint32
	}
	tests := []test{
		{2, 4, 1, []int{1}, []uint32{2}},
		{2, 2, 1, []int{1}, []uint32{2}},
		{3, 4, 8, []int{3, 3}, []uint32{4, 8}},
		{0, 8, 8, []int{8}, []uint32{0}},
		{0, 8, 7, []int{7}, []uint32{0}},
		{0, 8, 7, []int{3, 3}, []uint32{0, 4}},
		{1, 8, 8, []int{3, 3}, []uint32{2, 6}},
		{1, 8, 32, []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}},
		{3, 8, 16, []int{5, 7}, []uint32{4, 12}},
		{0, 8, 29, []int{5, 5, 5, 5}, []uint32{0, 8, 16, 24}},
		{0, 8, 10, []int{10}, []uint32{0}},
		{0, 8, 22, []int{10, 10}, []uint32{0, 12}},
		{1, 8, 25, []int{10, 10}, []uint32{4, 16}},
		{2, 8, 24, []int{10, 10}, []uint32{4, 16}},
		{0, 8, 55, []int{21, 31}, []uint32{0, 24}},
		{0, 8, 128, []int{64, 64}, []uint32{0, 64}},
		{0, appconsts.DefaultMaxSquareSize, 1000, []int{1000}, []uint32{0}},
		{0, appconsts.DefaultMaxSquareSize, appconsts.DefaultMaxSquareSize + 1, []int{appconsts.DefaultMaxSquareSize + 1}, []uint32{0}},
		{1, 128, 399, []int{128, 128, 128}, []uint32{16, 144, 272}},
		{1024, appconsts.DefaultMaxSquareSize, 32, []int{32}, []uint32{1024}},
	}
	for i, tt := range tests {
		res, indexes := BlobSharesUsedNonInteractiveDefaults(tt.cursor, tt.squareSize, tt.blobLens...)
		test := fmt.Sprintf("test %d: cursor %d, squareSize %d", i, tt.cursor, tt.squareSize)
		assert.Equal(t, tt.expected, res, test)
		assert.Equal(t, tt.indexes, indexes, test)
	}
}

func TestFitsInSquare(t *testing.T) {
	type test struct {
		name  string
		blobs []int
		start int
		size  int
		fits  bool
	}
	tests := []test{
		{
			name:  "1 blobs size 2 shares (2 blob shares, 2 compact, size 4)",
			blobs: []int{2},
			start: 2,
			size:  4,
			fits:  true,
		},
		{
			name:  "10 blobs size 10 shares (100 blob shares, 0 compact, size 4)",
			blobs: []int{10, 10, 10, 10, 10, 10, 10, 10, 10, 10},
			start: 0,
			size:  4,
			fits:  false,
		},
		{
			name:  "15 blobs size 1 share (15 blob shares, 0 compact, size 4)",
			blobs: []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			start: 0,
			size:  4,
			fits:  true,
		},
		{
			name:  "15 blobs size 1 share starting at share 2 (15 blob shares, 2 compact, size 4)",
			blobs: []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			start: 2,
			size:  4,
			fits:  false,
		},
		{
			name:  "8 blobs of various sizes (48 blob shares, 1 compact share, size 8)",
			blobs: []int{3, 9, 3, 7, 8, 3, 7, 8},
			start: 1,
			size:  8,
			fits:  true,
		},
		{
			// C = compact share
			// P = padding share
			//
			// |C|C|C|C|C|C|P|P|
			// |3|3|3|P|9|9|9|9|
			// |9|9|9|9|9|P|P|P|
			// |3|3|3|P|7|7|7|7|
			// |7|7|7|P|8|8|8|8|
			// |8|8|8|8|3|3|3|P|
			// |7|7|7|7|7|7|7|P|
			// |8|8|8|8|8|8|8|8|
			name:  "8 blobs of various sizes (48 blob shares, 6 compact, size 8)",
			blobs: []int{3, 9, 3, 7, 8, 3, 7, 8},
			start: 6,
			size:  8,
			fits:  true,
		},
		{
			name:  "0 blobs (0 blob shares, 5 compact, size 2)",
			blobs: []int{},
			start: 5,
			size:  2,
			fits:  false,
		},
		{
			name:  "0 blobs (0 blob shares, 4 compact, size 2)",
			blobs: []int{},
			start: 4,
			size:  2,
			fits:  true,
		},
		{
			name:  "0 blobs. Cursor at the the max share index",
			blobs: []int{},
			start: 16,
			size:  4,
			fits:  true,
		},
		{
			name:  "0 blobs. Cursor higher than max share index",
			blobs: []int{},
			start: 17,
			size:  4,
			fits:  false,
		},
		{
			name:  "0 blobs. Cursor higher than max share index (again)",
			blobs: []int{},
			start: 18,
			size:  4,
			fits:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, _ := FitsInSquare(tt.start, tt.size, tt.blobs...)
			assert.Equal(t, tt.fits, res)
		})
	}
}

func TestNextMultipleOfBlobMinSquareSize(t *testing.T) {
	type test struct {
		name                        string
		cursor, blobLen, squareSize int
		expectedIndex               int
		fits                        bool
	}
	tests := []test{
		{
			name:          "whole row blobLen 4",
			cursor:        0,
			blobLen:       4,
			squareSize:    4,
			fits:          true,
			expectedIndex: 0,
		},
		{
			name:          "half row blobLen 2 cursor 1",
			cursor:        1,
			blobLen:       2,
			squareSize:    4,
			fits:          true,
			expectedIndex: 2,
		},
		{
			name:          "half row blobLen 2 cursor 2",
			cursor:        2,
			blobLen:       2,
			squareSize:    4,
			fits:          true,
			expectedIndex: 2,
		},
		{
			name:          "half row blobLen 4 cursor 3",
			cursor:        3,
			blobLen:       4,
			squareSize:    8,
			fits:          true,
			expectedIndex: 4,
		},
		{
			name:          "blobLen 5 cursor 3 size 8",
			cursor:        3,
			blobLen:       5,
			squareSize:    8,
			fits:          false,
			expectedIndex: 4,
		},
		{
			name:          "blobLen 2 cursor 3 square size 8",
			cursor:        3,
			blobLen:       2,
			squareSize:    8,
			fits:          true,
			expectedIndex: 4,
		},
		{
			name:          "cursor 3 blobLen 5 size 8",
			cursor:        3,
			blobLen:       5,
			squareSize:    8,
			fits:          false,
			expectedIndex: 4,
		},
		{
			name:          "bloblen 12 cursor 1 size 16",
			cursor:        1,
			blobLen:       12,
			squareSize:    16,
			fits:          true,
			expectedIndex: 4,
		},
		{
			name:          "edge case where there are many blobs with a single size",
			cursor:        10291,
			blobLen:       1,
			squareSize:    128,
			fits:          true,
			expectedIndex: 10291,
		},
		{
			name:          "second row blobLen 2 cursor 11 square size 8",
			cursor:        11,
			blobLen:       2,
			squareSize:    8,
			fits:          true,
			expectedIndex: 12,
		},
		{
			// inspired by the diagram at https://github.com/celestiaorg/celestia-app/blob/1b80b94a62c8c292f569e2fc576e26299985681a/docs/architecture/adr-009-non-interactive-default-rules-for-reduced-padding.md?plain=1#L30
			name:          "non-interactive default rules for reduced padding diagram",
			cursor:        11,
			blobLen:       11,
			squareSize:    8,
			fits:          false,
			expectedIndex: 12,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, fits := NextMultipleOfBlobMinSquareSize(tt.cursor, tt.blobLen, tt.squareSize)
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

func TestMinSquareSize(t *testing.T) {
	type testCase struct {
		shareCount int
		want       int
	}
	testCases := []testCase{
		{
			shareCount: 0,
			want:       1,
		},
		{
			shareCount: 1,
			want:       1,
		},
		{
			shareCount: 2,
			want:       2,
		},
		{
			shareCount: 3,
			want:       2,
		},
		{
			shareCount: 4,
			want:       2,
		},
		{
			shareCount: 5,
			want:       4,
		},
		{
			shareCount: 16,
			want:       4,
		},
		{
			shareCount: 17,
			want:       8,
		},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("shareCount %d", tc.shareCount), func(t *testing.T) {
			got := MinSquareSize(tc.shareCount)
			assert.Equal(t, tc.want, got)
		})
	}
}
