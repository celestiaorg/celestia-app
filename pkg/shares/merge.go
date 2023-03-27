package shares

import (
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/rsmt2d"
	coretypes "github.com/tendermint/tendermint/types"
)

// merge extracts block data from an extended data square.
// TODO: export this function
func merge(eds *rsmt2d.ExtendedDataSquare) (coretypes.Data, error) {
	squareSize := eds.Width() / 2

	// sort block data shares by namespace
	var (
		sortedTxShares    []Share
		sortedPfbTxShares []Share
		sortedBlobShares  []Share
	)

	// iterate over each row index
	for x := uint(0); x < squareSize; x++ {
		// iterate over each share in the original data square
		row := eds.Row(x)

		for _, shareBytes := range row[:squareSize] {
			// sort the data of that share types via namespace
			share, err := NewEmptyBuilder().ImportRawShare(shareBytes).Build()
			if err != nil {
				return coretypes.Data{}, err
			}
			ns, err := appns.From(share.data[:appns.NamespaceSize])
			if err != nil {
				return coretypes.Data{}, err
			}

			switch {
			case ns.IsTx():
				sortedTxShares = append(sortedTxShares, *share)
			case ns.IsPayForBlob():
				sortedPfbTxShares = append(sortedPfbTxShares, *share)
			case ns.IsTailPadding():
				continue

			// ignore unused but reserved namespaces
			case ns.IsReserved():
				continue

			// every other namespaceID should be a blob
			default:
				sortedBlobShares = append(sortedBlobShares, *share)
			}
		}
	}

	// pass the raw share data to their respective parsers
	ordinaryTxs, err := ParseTxs(sortedTxShares)
	if err != nil {
		return coretypes.Data{}, err
	}
	pfbTxs, err := ParseTxs(sortedPfbTxShares)
	if err != nil {
		return coretypes.Data{}, err
	}
	txs := append(ordinaryTxs, pfbTxs...)

	blobs, err := ParseBlobs(sortedBlobShares)
	if err != nil {
		return coretypes.Data{}, err
	}

	// TODO the Data returned below does not have the correct data.hash populated.
	return coretypes.Data{
		Txs:        txs,
		Blobs:      blobs,
		SquareSize: uint64(squareSize),
	}, nil
}
