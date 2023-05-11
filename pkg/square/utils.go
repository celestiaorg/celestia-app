package square

import "github.com/celestiaorg/celestia-app/pkg/appconsts"

// EstimateMaxBlockBytes estimates the maximum number of bytes a block can have.
// This value overestimates by < .1% and should be treated as such. Each block
// has different amounts of overhead based on how many blobs and transactions it
// contains.
func EstimateMaxBlockBytes(squareSize uint64) int64 {
	return int64(squareSize * squareSize * appconsts.ContinuationSparseShareContentSize)
}
