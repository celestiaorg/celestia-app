package square

import "github.com/celestiaorg/celestia-app/pkg/appconsts"

// EstimateMaxBlockBytes estimates the maximum number of bytes a block can have.
// This function overestimates by < .1% and the value produced does not
// guarantee that a square of the desired size will always be created using the
// resulting parameter. Each block has different amounts of overhead based on
// how many blobs and transactions it contains.
func EstimateMaxBlockBytes(squareSize uint64) int64 {
	bsize := squareSize * squareSize * appconsts.ContinuationSparseShareContentSize
	return int64(bsize)
}
