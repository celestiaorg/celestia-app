package app

import (
	"bytes"
	"errors"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/shares"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

type trackedBlob struct {
	blob        tmproto.Blob
	parsedIndex int
	sharesUsed  int
}

func addShareIndexes(squareSize uint64, nonreserveStart int, ptxs []parsedTx) ([]parsedTx, error) {
	maxShareCount := squareSize * squareSize
	if nonreserveStart > int(maxShareCount) {
		return nil, errors.New("non reserver start index greater than max share count")
	}
	// we want to preserve the order of the txs, but we need to sort the blobs
	// by namespace, so we separate them and
	var trackedBlobs []trackedBlob
	for i, pTx := range ptxs {
		if pTx.normalTx != nil {
			continue
		}
		trackedBlobs = append(trackedBlobs, trackedBlob{
			blob:        pTx.blobTx.Blobs[0],
			parsedIndex: i,
			sharesUsed:  shares.MsgSharesUsed(pTx.blobTx.DataUsed()),
		})
	}

	sort.SliceStable(trackedBlobs, func(i, j int) bool {
		return bytes.Compare(trackedBlobs[i].blob.NamespaceId, trackedBlobs[j].blob.NamespaceId) < 0
	})

	blobShareLens := make([]int, len(trackedBlobs))
	for i, b := range trackedBlobs {
		blobShareLens[i] = b.sharesUsed
	}

	sharesUsed, blobStartIndexes := shares.MsgSharesUsedNonInteractiveDefaults(
		nonreserveStart,
		int(squareSize),
		blobShareLens...,
	)

	if sharesUsed+nonreserveStart >= int(maxShareCount) {
		ptxs, blobStartIndexes = pruneExcessBlobs(squareSize, nonreserveStart, ptxs, trackedBlobs, blobStartIndexes)
	}

	// add the share indexes back to the parsed transactions
	for i, tBlob := range trackedBlobs {
		ptxs[tBlob.parsedIndex].shareIndex = blobStartIndexes[i]
	}

	return ptxs, nil
}

// pruneExcessBlobs will prune excess parsedTxs and their blobs until they fit
// in the square.
//
// TODO: refactor to use a more sophisticated pruning algo so that we don't just
// prune the largest namespaces
func pruneExcessBlobs(
	squareSize uint64,
	nonreserveStart int,
	ptxs []parsedTx,
	sortedBlobs []trackedBlob,
	shareIndexes []uint32,
) ([]parsedTx, []uint32) {
	maxShares := int(squareSize * squareSize)
	for i := len(sortedBlobs) - 1; i >= 0; i-- {
		lastShareIndex := sortedBlobs[0].parsedIndex + sortedBlobs[0].sharesUsed
		if lastShareIndex < maxShares {
			break
		}

		// remove the last blob and tx
		shareIndexes = remove[uint32](shareIndexes, i)

	}

	return ptxs, shareIndexes
}

func remove[T any](p []T, i int) []T {
	if i >= len(p) {
		return p
	}
	copy(p[i:], p[i+1:])
	return p[:len(p)-1]
}
