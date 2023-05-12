package shares

import (
	"errors"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	coretypes "github.com/tendermint/tendermint/types"
	"golang.org/x/exp/maps"
)

var (
	ErrIncorrectNumberOfIndexes = errors.New(
		"number of indexes is not identical to the number of blobs",
	)
	ErrUnexpectedFirstBlobShareIndex = errors.New(
		"the first blob started at an unexpected index",
	)
)

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

func SplitTxs(txs coretypes.Txs) (txShares []Share, pfbShares []Share, shareRanges map[coretypes.TxKey]ShareRange, err error) {
	txWriter := NewCompactShareSplitter(appns.TxNamespace, appconsts.ShareVersionZero)
	pfbTxWriter := NewCompactShareSplitter(appns.PayForBlobNamespace, appconsts.ShareVersionZero)

	for _, tx := range txs {
		if _, isIndexWrapper := coretypes.UnmarshalIndexWrapper(tx); isIndexWrapper {
			err = pfbTxWriter.WriteTx(tx)
		} else {
			err = txWriter.WriteTx(tx)
		}
		if err != nil {
			return nil, nil, nil, err
		}
	}

	txShares, err = txWriter.Export()
	if err != nil {
		return nil, nil, nil, err
	}
	txMap := txWriter.ShareRanges(0)

	pfbShares, err = pfbTxWriter.Export()
	if err != nil {
		return nil, nil, nil, err
	}
	pfbMap := pfbTxWriter.ShareRanges(len(txShares))

	return txShares, pfbShares, mergeMaps(txMap, pfbMap), nil
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
			if err := writer.WriteNamespacedPaddedShares(paddedShareCount); err != nil {
				return nil, err
			}
		}
	}
	return writer.Export(), nil
}

// mergeMaps merges two maps into a new map. If there are any duplicate keys,
// the value in the second map takes precedence.
func mergeMaps(mapOne, mapTwo map[coretypes.TxKey]ShareRange) map[coretypes.TxKey]ShareRange {
	merged := make(map[coretypes.TxKey]ShareRange, len(mapOne)+len(mapTwo))
	maps.Copy(merged, mapOne)
	maps.Copy(merged, mapTwo)
	return merged
}
