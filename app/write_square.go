package app

import (
	"bytes"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/shares"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

type trackedBlob struct {
	blob *tmproto.Blob
	// parsedIndex keeps track of which parsed transaction a blob relates to
	parsedIndex int
	// blobIndex keeps track of which blob in a parsed transaction
	// this blob relates to
	blobIndex  int
	sharesUsed int
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
		ptxs[i].shareIndexes = make([]uint32, len(pTx.blobTx.Blobs))
		for j, blob := range pTx.blobTx.Blobs {
			trackedBlobs = append(trackedBlobs, trackedBlob{
				blob:        blob,
				parsedIndex: i,
				blobIndex:   j,
				sharesUsed:  shares.SparseSharesNeeded(uint32(len(blob.Data))),
			})
		}
	}

	// blobs must be sorted by namespace in order for nmt to be able to create a
	// commitment over each row and column.
	sort.SliceStable(trackedBlobs, func(i, j int) bool {
		return bytes.Compare(trackedBlobs[i].blob.NamespaceId, trackedBlobs[j].blob.NamespaceId) < 0
	})

	cursor := nonreserveStart
	iSS := int(squareSize)
	maxSharesSize := iSS * iSS
	removeIndexes := make(map[int]bool)
	for _, tBlob := range trackedBlobs {
		// skip this blob, as it will already be removed
		if removeIndexes[tBlob.parsedIndex] {
			continue
		}
		cursor, _ = shares.NextMultipleOfBlobMinSquareSize(cursor, tBlob.sharesUsed, iSS)
		// remove the parsed transaction if it cannot fit into the square
		if cursor+tBlob.sharesUsed > maxSharesSize {
			removeIndexes[tBlob.parsedIndex] = true
			continue
		}
		// set the share index in the same order as the blob that its for
		ptxs[tBlob.parsedIndex].shareIndexes[tBlob.blobIndex] = uint32(cursor)
		cursor += tBlob.sharesUsed
	}

	ptxs = removeMany(ptxs, removeIndexes)

	blobTxCount := 0
	for _, ptx := range ptxs {
		if len(ptx.blobTx.Tx) != 0 {
			blobTxCount++
		}
	}

	derefBlobs := make([]tmproto.Blob, 0)
	for _, ptx := range ptxs {
		if len(ptx.normalTx) != 0 {
			continue
		}
		for _, blob := range ptx.blobTx.Blobs {
			derefBlobs = append(derefBlobs, *blob)
		}
	}

	// todo: don't sort twice
	sort.SliceStable(derefBlobs, func(i, j int) bool {
		return bytes.Compare(derefBlobs[i].NamespaceId, derefBlobs[j].NamespaceId) < 0
	})

	return ptxs, derefBlobs
}

func removeMany[T any](s []T, removeIndexes map[int]bool) []T {
	// Create a new slice to store the remaining elements
	result := make([]T, 0, len(s)-len(removeIndexes))

	// Iterate over the original slice and append elements
	// to the result slice unless they are marked for removal
	for i, x := range s {
		if !removeIndexes[i] {
			result = append(result, x)
		}
	}

	return result
}
