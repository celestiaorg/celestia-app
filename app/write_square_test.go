package app

import (
	"bytes"
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmlog "github.com/tendermint/tendermint/libs/log"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func Test_finalizeLayout(t *testing.T) {
	ns1 := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	ns2 := []byte{2, 2, 2, 2, 2, 2, 2, 2}
	ns3 := []byte{3, 3, 3, 3, 3, 3, 3, 3}

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
				[][]byte{ns1},
				[]int{1},
			),
			expectedIndexes: []uint32{10},
		},
		{
			squareSize:      4,
			nonreserveStart: 10,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns1, ns1},
				[]int{100, 100},
			),
			expectedIndexes: []uint32{10, 11},
		},
		{
			squareSize:      4,
			nonreserveStart: 10,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns1, ns1, ns1, ns1, ns1, ns1, ns1, ns1, ns1, ns1},
				[]int{100, 100, 100, 100, 100, 100, 100, 100, 100, 100},
			),
			expectedIndexes: []uint32{10, 11, 12, 13, 14, 15},
		},
		{
			squareSize:      4,
			nonreserveStart: 7,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns1, ns1, ns1, ns1, ns1, ns1, ns1, ns1, ns1},
				[]int{100, 100, 100, 100, 100, 100, 100, 100, 100},
			),
			expectedIndexes: []uint32{7, 8, 9, 10, 11, 12, 13, 14, 15},
		},
		{
			squareSize:      4,
			nonreserveStart: 3,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns1, ns1, ns1},
				[]int{10000, 10000, 1000000},
			),
			expectedIndexes: []uint32{},
		},
		{
			squareSize:      64,
			nonreserveStart: 32,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns1, ns1, ns1},
				[]int{1000, 10000, 100000}, // blob share lengths of 2, 20, 199 respectively
			),
			expectedIndexes: []uint32{
				// BlobMinSquareSize(2) = 2 so the first blob has to start at the
				// next multiple of 2 >= 32 which is 32. This blob occupies
				// shares 32 to 33.
				32,
				// BlobMinSquareSize(20) = 8 so the second blob has to start at
				// the next multiple of 8 >= 34 which is 40. This blob occupies
				// shares 40 to 59.
				40,
				// BlobMinSquareSize(199) = 16 so the third blob has to start at
				// the next multiple of 16 >= 60 which is 64. This blob occupies
				// shares 64 to 262.
				64,
			},
		},
		{
			squareSize:      32,
			nonreserveStart: 32,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns2, ns1, ns1},
				[]int{100, 100, 100},
			),
			expectedIndexes: []uint32{34, 32, 33},
		},
		{
			squareSize:      32,
			nonreserveStart: 32,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns1, ns2, ns1},
				[]int{100, 1000, 1000},
			),
			expectedIndexes: []uint32{32, 36, 34},
		},
		{
			squareSize:      32,
			nonreserveStart: 32,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns1, ns2, ns1},
				[]int{100, 1000, 1000},
			),
			expectedIndexes: []uint32{32, 36, 34},
		},
		{
			squareSize:      4,
			nonreserveStart: 2,
			ptxs: generateParsedTxsWithNIDs(
				[][]byte{ns1, ns3, ns2},
				[]int{100, 1000, 420},
			),
			expectedIndexes: []uint32{2, 4, 3},
		},
	}
	for i, tt := range tests {
		res, blobs := finalizeLayout(tt.squareSize, tt.nonreserveStart, tt.ptxs)
		require.Equal(t, len(tt.expectedIndexes), len(res), i)
		require.Equal(t, len(tt.expectedIndexes), len(blobs), i)
		for i, ptx := range res {
			assert.Equal(t, tt.expectedIndexes[i], ptx.shareIndex, i)
		}

		processedTxs := processTxs(tmlog.NewNopLogger(), res)

		sort.SliceStable(blobs, func(i, j int) bool {
			return bytes.Compare(blobs[i].NamespaceId, blobs[j].NamespaceId) < 0
		})

		blockData := core.Data{
			Txs:        processedTxs,
			Blobs:      blobs,
			SquareSize: tt.squareSize,
		}

		coreData, err := coretypes.DataFromProto(&blockData)
		require.NoError(t, err)

		_, err = shares.Split(coreData, true)
		require.NoError(t, err)
	}
}
