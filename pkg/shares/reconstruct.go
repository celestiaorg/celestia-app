package shares

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/rsmt2d"
	coretypes "github.com/tendermint/tendermint/types"
)

// reconstruct reconstructs block data from an extended data square.
// TODO: export this function
func reconstruct(eds *rsmt2d.ExtendedDataSquare) (coretypes.Data, error) {
	squareSize := eds.Width() / 2

	// sort block data shares by namespace
	var (
		sortedTxShares   [][]byte
		sortedBlobShares [][]byte
	)

	// iterate over each row index
	for x := uint(0); x < squareSize; x++ {
		// iterate over each share in the original data square
		row := eds.Row(x)

		for _, share := range row[:squareSize] {
			// sort the data of that share types via namespace
			nid := share[:appconsts.NamespaceSize]
			switch {
			case bytes.Equal(appconsts.TxNamespaceID, nid):
				sortedTxShares = append(sortedTxShares, share)

			case bytes.Equal(appconsts.TailPaddingNamespaceID, nid):
				continue

			// ignore unused but reserved namespaces
			case bytes.Compare(nid, appconsts.MaxReservedNamespace) < 1:
				continue

			// every other namespaceID should be a blob
			default:
				sortedBlobShares = append(sortedBlobShares, share)
			}
		}
	}

	// pass the raw share data to their respective parsers
	txs, err := ParseTxs(sortedTxShares)
	if err != nil {
		return coretypes.Data{}, err
	}

	blobs, err := ParseBlobs(sortedBlobShares)
	if err != nil {
		return coretypes.Data{}, err
	}

	return coretypes.Data{
		Txs:        txs,
		Blobs:      blobs,
		SquareSize: uint64(squareSize),
	}, nil
}
