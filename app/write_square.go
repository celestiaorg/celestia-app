package app

import (
	"bytes"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/tendermint/tendermint/pkg/consts"
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

// finalizeLayout returns the PFB transactions and their respective blobs in their completed layout.
// Valid square layouts only include a MsgPayForBlobs transaction if the blob is
// also included in the square. The blobs are sorted by namespace.
func finalizeBlobLayout(squareSize uint64, nonreserveStart int, blobTxs []tmproto.BlobTx) ([][]byte, []tmproto.Blob) {
	// we split the transactions from the blobs here, but keep track of which
	// parsed transaction the blobs originated from. transactions must only be
	// added to the square if their respective blob(s) are also added to the square.
	// Also, the blobs must be sorted by namespace before we can split them into
	// shares and create nmt commitments over each row and column.
	trackedBlobs := make([]*trackedBlob, 0)
	wrappedPFBs := make([]tmproto.IndexWrapper, len(blobTxs))
	for i, blobTx := range blobTxs {
		wrappedPFBs[i] = tmproto.IndexWrapper{
			Tx:           blobTx.Tx,
			TypeId:       consts.ProtoIndexWrapperTypeID,
			ShareIndexes: make([]uint32, len(blobTx.Blobs)),
		}
		for j, blob := range blobTx.Blobs {
			trackedBlobs = append(trackedBlobs, &trackedBlob{
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
	removePFBIndexes := make(map[int]bool)
	for _, tBlob := range trackedBlobs {
		// skip this blob, as it will already be removed due to another blob in the
		// same PFB being removed
		if removePFBIndexes[tBlob.parsedIndex] {
			continue
		}
		cursor, _ = shares.NextMultipleOfBlobMinSquareSize(cursor, tBlob.sharesUsed, iSS)
		// remove the parsed transaction if it cannot fit into the square
		if cursor+tBlob.sharesUsed > maxSharesSize {
			removePFBIndexes[tBlob.parsedIndex] = true
			continue
		}
		// set the share index in the same order as the blob that its for
		wrappedPFBs[tBlob.parsedIndex].ShareIndexes[tBlob.blobIndex] = uint32(cursor)
		cursor += tBlob.sharesUsed
	}

	blobs := make([]tmproto.Blob, 0, len(trackedBlobs))
	for _, tBlob := range trackedBlobs {
		if removePFBIndexes[tBlob.parsedIndex] {
			continue
		}
		blobs = append(blobs, *tBlob.blob)
	}

	// prune the PFBs that didn't make it and then marshal them to bytes
	wrappedPFBTxs := make([][]byte, len(wrappedPFBs)-len(removePFBIndexes))
	index := 0
	for idx, pfb := range wrappedPFBs {
		if removePFBIndexes[idx] {
			continue
		}
		pfbBytes, err := pfb.Marshal()
		if err != nil {
			panic(err)
		}
		wrappedPFBTxs[index] = pfbBytes
		index++
	}

	return wrappedPFBTxs, blobs
}
