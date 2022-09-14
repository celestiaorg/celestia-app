package inclusion

import (
	"errors"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/tendermint/tendermint/crypto/merkle"
)

func GetCommit(cacher *EDSSubTreeRootCacher, dah da.DataAvailabilityHeader, start, msgShareLen int) ([]byte, error) {
	originalSquareSize := len(dah.RowsRoots) / 2
	if start+msgShareLen > originalSquareSize*originalSquareSize {
		return nil, errors.New("cannot get commit for message that doesn't fit in square")
	}
	paths := calculateCommitPaths(originalSquareSize, start, msgShareLen)
	commits := make([][]byte, len(paths))
	for i, path := range paths {
		// here we prepend false (walk left down the tree) because we only need
		// the commits to the original square
		orignalSquarePath := append(append(make([]WalkInstruction, 0, len(path.instructions)+1), WalkLeft), path.instructions...)
		commit, err := cacher.getSubTreeRoot(dah, path.row, orignalSquarePath)
		if err != nil {
			return nil, err
		}
		commits[i] = commit

	}
	return merkle.HashFromByteSlices(commits), nil
}
