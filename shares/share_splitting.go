package shares

import (
	"errors"

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

func SplitTxs(txs coretypes.Txs) (txShares []Share, pfbShares []Share, shareRanges map[coretypes.TxKey]Range, err error) {
	txWriter := NewCompactShareSplitter(TxNamespace, ShareVersionZero)
	pfbTxWriter := NewCompactShareSplitter(PayForBlobNamespace, ShareVersionZero)

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

// SplitBlobs splits the provided blobs into shares.
func SplitBlobs(blobs ...*Blob) ([]Share, error) {
	writer := NewSparseShareSplitter()
	for _, blob := range blobs {
		if err := writer.Write(blob); err != nil {
			return nil, err
		}
	}
	return writer.Export(), nil
}

// mergeMaps merges two maps into a new map. If there are any duplicate keys,
// the value in the second map takes precedence.
func mergeMaps(mapOne, mapTwo map[coretypes.TxKey]Range) map[coretypes.TxKey]Range {
	merged := make(map[coretypes.TxKey]Range, len(mapOne)+len(mapTwo))
	maps.Copy(merged, mapOne)
	maps.Copy(merged, mapTwo)
	return merged
}
