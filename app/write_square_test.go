package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addShareIndexes(t *testing.T) {
	type result struct {
		ptxs         []parsedTx
		shareIndexes []uint32
	}

	ns := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	type test struct {
		squareSize      uint64
		nonreserveStart int
		ptxs            []parsedTx
		expectedIndexes []uint32
	}
	tests := []test{
		{
			squareSize:      4,
			nonreserveStart: 10,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns},
				[]int{1},
			),
			expectedIndexes: []uint32{10},
		},
		{
			squareSize:      4,
			nonreserveStart: 10,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns, ns},
				[]int{100, 100},
			),
			expectedIndexes: []uint32{10, 11},
		},
		{
			squareSize:      4,
			nonreserveStart: 10,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns, ns, ns, ns, ns, ns, ns, ns, ns, ns},
				[]int{100, 100, 100, 100, 100, 100, 100, 100, 100, 100},
			),
			expectedIndexes: []uint32{10, 11, 12, 13, 14, 15},
		},
		{
			squareSize:      4,
			nonreserveStart: 2,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns, ns, ns, ns, ns, ns, ns, ns, ns, ns},
				[]int{100, 100, 100, 100, 100, 100, 100, 100, 100, 100},
			),
			expectedIndexes: []uint32{2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		},
		{
			squareSize:      4,
			nonreserveStart: 3,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns, ns, ns},
				[]int{10000, 10000, 1000000},
			),
			expectedIndexes: []uint32{},
		},
		{
			squareSize:      64,
			nonreserveStart: 32,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns, ns, ns},
				[]int{1000, 10000, 100000},
			),
			expectedIndexes: []uint32{32, 48, 128},
		},
	}
	for _, tt := range tests {
		res, err := addShareIndexes(tt.squareSize, tt.nonreserveStart, tt.ptxs)
		assert.NoError(t, err)
		require.Equal(t, len(tt.expectedIndexes), len(res))
		for i, ptx := range res {
			assert.Equal(t, tt.expectedIndexes[i], ptx.shareIndex)
		}
	}
}
