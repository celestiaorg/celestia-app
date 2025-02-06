package inclusion

import (
	"errors"

	"github.com/celestiaorg/celestia-app/v3/pkg/da"
	"github.com/cometbft/cometbft/crypto/merkle"
)

// GetCommitment gets the share commitment for a blob in the original data
// square.
func GetCommitment(cacher *EDSSubTreeRootCacher, dah da.DataAvailabilityHeader, start, blobShareLen, subtreeRootThreshold int) ([]byte, error) {
	squareSize := len(dah.RowRoots) / 2
	if start+blobShareLen > squareSize*squareSize {
		return nil, errors.New("cannot get commitment for blob that doesn't fit in square")
	}
	paths := calculateCommitmentPaths(squareSize, start, blobShareLen, subtreeRootThreshold)
	subTreeRoots := make([][]byte, len(paths))
	for i, path := range paths {
		// here we prepend false (walk left down the tree) because we only need
		// the subtree roots from the original data square.
		originalSquarePath := append(append(make([]WalkInstruction, 0, len(path.instructions)+1), WalkLeft), path.instructions...)
		subTreeRoot, err := cacher.getSubTreeRoot(dah, path.row, originalSquarePath)
		if err != nil {
			return nil, err
		}
		subTreeRoots[i] = subTreeRoot
	}
	return merkle.HashFromByteSlices(subTreeRoots), nil
}
