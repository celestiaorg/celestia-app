package shares

import (
	"errors"
	"fmt"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	coretypes "github.com/tendermint/tendermint/types"
)

var (
	ErrIncorrectNumberOfIndexes = errors.New(
		"number of indexes is not identical to the number of blobs",
	)
	ErrUnexpectedFirstBlobShareIndex = errors.New(
		"the first blob started at an unexpected index",
	)
)

// Split converts block data into encoded shares, optionally using share indexes
// that are encoded as wrapped transactions. Most use cases out of this package
// should use these share indexes and therefore set useShareIndexes to true.
func Split(data coretypes.Data, useShareIndexes bool) ([]Share, error) {
	if data.SquareSize == 0 || !isPowerOf2(data.SquareSize) {
		return nil, fmt.Errorf("square size is not a power of two: %d", data.SquareSize)
	}
	wantShareCount := int(data.SquareSize * data.SquareSize)
	currentShareCount := 0

	txShares := SplitTxs(data.Txs)
	currentShareCount += len(txShares)
	// blobIndexes will be nil if we are working with a list of txs that do not
	// have a blob index. This preserves backwards compatibility with old blocks
	// that do not follow the non-interactive defaults
	blobIndexes := ExtractShareIndexes(data.Txs)
	sort.Slice(blobIndexes, func(i, j int) bool { return blobIndexes[i] < blobIndexes[j] })

	var padding []Share
	if len(data.Blobs) > 0 {
		blobShareStart, _ := NextMultipleOfBlobMinSquareSize(
			currentShareCount,
			SparseSharesNeeded(uint32(len(data.Blobs[0].Data))),
			int(data.SquareSize),
		)
		// force blobSharesStart to be the first share index
		if len(blobIndexes) != 0 && useShareIndexes {
			blobShareStart = int(blobIndexes[0])
		}

		padding = namespacedPaddedShares(appconsts.TailTransactionPaddingNamespaceID, blobShareStart-currentShareCount)
	}
	currentShareCount += len(padding)

	if blobIndexes != nil && int(blobIndexes[0]) < currentShareCount {
		return nil, ErrUnexpectedFirstBlobShareIndex
	}

	blobShares, err := SplitBlobs(currentShareCount, blobIndexes, data.Blobs, useShareIndexes)
	if err != nil {
		return nil, err
	}
	currentShareCount += len(blobShares)
	tailShares := TailPaddingShares(wantShareCount - currentShareCount)
	shares := make([]Share, 0, data.SquareSize*data.SquareSize)
	shares = append(append(append(append(
		shares,
		txShares...),
		padding...),
		blobShares...),
		tailShares...)
	return shares, nil
}

// ExtractShareIndexes iterates over the transactions and extracts the share
// indexes from wrapped transactions. It returns nil if the transactions are
// from an old block that did not have share indexes in the wrapped txs.
func ExtractShareIndexes(txs coretypes.Txs) []uint32 {
	var shareIndexes []uint32
	for _, rawTx := range txs {
		if indexWrappedTxs, isIndexWrapped := coretypes.UnmarshalIndexWrapper(rawTx); isIndexWrapped {
			// Since share index == 0 is invalid, it indicates that we are
			// attempting to extract share indexes from txs that do not have any
			// due to them being old. here we return nil to indicate that we are
			// attempting to extract indexes from a block that doesn't support
			// it. It checks for 0 because if there is a message in the block,
			// then there must also be a tx, which will take up at least one
			// share.
			if len(indexWrappedTxs.ShareIndexes) == 0 {
				return nil
			}
			shareIndexes = append(shareIndexes, indexWrappedTxs.ShareIndexes...)
		}
	}

	return shareIndexes
}

func SplitTxs(txs coretypes.Txs) []Share {
	writer := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersionZero)
	for _, tx := range txs {
		writer.WriteTx(tx)
	}
	return writer.Export()
}

func SplitBlobs(cursor int, indexes []uint32, blobs []coretypes.Blob, useShareIndexes bool) ([]Share, error) {
	if useShareIndexes && len(indexes) != len(blobs) {
		return nil, ErrIncorrectNumberOfIndexes
	}
	writer := NewSparseShareSplitter()
	for i, blob := range blobs {
		if err := writer.Write(blob); err != nil {
			return nil, err
		}
		if useShareIndexes && len(indexes) > i+1 {
			paddedShareCount := int(indexes[i+1]) - (writer.Count() + cursor)
			writer.WriteNamespacedPaddedShares(paddedShareCount)
		}
	}
	return writer.Export(), nil
}
