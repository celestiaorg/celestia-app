package inclusion

import (
	"errors"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/tendermint/tendermint/crypto/merkle"
)

func GetMultiCommit(cacher *EDSSubTreeRootCacher, dah da.DataAvailabilityHeader, startIndexes, lengths []uint32) ([]byte, error) {
	shareCounts := make([]int, len(lengths))
	for i := range lengths {
		shareCounts[i] = shares.SparseSharesNeeded(uint32(lengths[i]))
	}
	commitments := make([][]byte, len(startIndexes))
	for i := range startIndexes {
		commitment, err := GetCommit(cacher, dah, int(startIndexes[i]), shareCounts[i])
		if err != nil {
			return nil, err
		}
		commitments[i] = commitment
	}
	return merkle.HashFromByteSlices(commitments), nil
}

func GetCommit(cacher *EDSSubTreeRootCacher, dah da.DataAvailabilityHeader, start, blobShareLen int) ([]byte, error) {
	squareSize := len(dah.RowsRoots) / 2
	if start+blobShareLen > squareSize*squareSize {
		return nil, errors.New("cannot get commit for blob that doesn't fit in square")
	}
	paths := calculateCommitPaths(squareSize, start, blobShareLen)
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
