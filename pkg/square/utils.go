package square

import "github.com/celestiaorg/celestia-app/pkg/appconsts"

// EstimateMaxBlockBytes estimates the maximum number of bytes a block can have.
// The value produced does not guarantee that a square of the desired size will
// always be created using the resulting parameter, however it has been fuzzed
// extensively to work for a 64 x 64 square.
//
// NOTE: We make an assumption that 6.25% of the block, not counting namespaces,
// will be overhead. Values lower than that will occasionally fail the fuzz test
// for squares of size 64.
//
// Note that we can't actually come up with a worst case value here that is also
// efficiently fills the square, because the worst case scenario involves many
// individual blobs of size 1 byte. Since each blob fills the square is a sparse
// way, each will be padded to single share. Since tendermint doesn't gossip or
// know about that padding, it will reap many txs from the mempool that will
// cause the square to be increased beyond the desired size.
func EstimateMaxBlockBytes(squareSize uint64) int64 {
	bsize := squareSize * squareSize * appconsts.ContinuationSparseShareContentSize
	bsize = bsize - bsize/16
	return int64(bsize)
}
