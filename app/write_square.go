package app

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/shares"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

type trackedBlob struct {
	blob        tmproto.Blob
	parsedIndex int
	sharesUsed  int
}

// finalizeLayout returns the transactions and blobs in their completed layout.
// Valid square layouts only include a MsgPayForBlob transaction if the blob is
// also included in the square. The blobs are also sorted by namespace.
func finalizeLayout(squareSize uint64, nonreserveStart int, ptxs []parsedTx) ([]parsedTx, []tmproto.Blob) {
	// we split the transactions from the blobs here, but keep track of which
	// parsed transaction the blobs originated from. transactions must only be
	// added to the square if their respective blob is also added to the square.
	// Also, the blobs must be sorted by namespace before we can split them into
	// shares and create nmt commitments over each row and column.
	trackedBlobs := make([]trackedBlob, 0)
	for i, pTx := range ptxs {
		if len(pTx.normalTx) != 0 {
			continue
		}
		trackedBlobs = append(trackedBlobs, trackedBlob{
			blob:        pTx.blobTx.Blobs[0],
			parsedIndex: i,
			sharesUsed:  shares.SparseSharesNeeded(uint32(pTx.blobTx.DataUsed())),
		})
	}

	// blobs must be sorted by namespace in order for nmt to be able to create a
	// commitment over each row and column.
	sort.SliceStable(trackedBlobs, func(i, j int) bool {
		return bytes.Compare(trackedBlobs[i].blob.NamespaceId, trackedBlobs[j].blob.NamespaceId) < 0
	})

	cursor := nonreserveStart
	iSS := int(squareSize)
	maxSharesSize := iSS * iSS
	blobs := make([]tmproto.Blob, 0)
	removeList := []int{}
	for _, tBlob := range trackedBlobs {
		cursor, _ = shares.NextAlignedPowerOfTwo(cursor, tBlob.sharesUsed, iSS)
		// remove the parsed transaction if it cannot fit into the square
		if cursor+tBlob.sharesUsed > maxSharesSize {
			removeList = append(removeList, tBlob.parsedIndex)
			continue
		}
		ptxs[tBlob.parsedIndex].shareIndex = uint32(cursor)
		blobs = append(blobs, tBlob.blob)
		cursor += tBlob.sharesUsed
	}

	ptxs = removeMany(ptxs, removeList...)

	blobTxCount := 0
	for _, ptx := range ptxs {
		if len(ptx.blobTx.Tx) != 0 {
			blobTxCount++
		}
	}

	if blobTxCount != len(blobs) {
		panic(fmt.Sprintf("invalid number of blob txs: must be equal to number of blobs: txs %d blobs %d", blobTxCount, len(blobs)))
	}

	return ptxs, blobs
}

func removeMany[T any](s []T, indexes ...int) []T {
	// Create a map to track which indexes to remove
	remove := make(map[int]bool)
	for _, i := range indexes {
		remove[i] = true
	}

	// Create a new slice to store the remaining elements
	result := make([]T, 0, len(s)-len(indexes))

	// Iterate over the original slice and append elements
	// to the result slice unless they are marked for removal
	for i, x := range s {
		if !remove[i] {
			result = append(result, x)
		}
	}

	return result
}
