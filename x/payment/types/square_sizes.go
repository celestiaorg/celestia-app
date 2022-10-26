package types

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
)

// https://github.com/celestiaorg/celestia-app/issues/236
// https://github.com/celestiaorg/celestia-app/issues/239

var allSquareSizes = generateAllSquareSizes()

// generateAllSquareSizes generates and returns all of the possible square sizes
// using the maximum and minimum square sizes
func generateAllSquareSizes() []int {
	sizes := []int{}
	cursor := int(appconsts.MinSquareSize)
	for cursor <= appconsts.MaxSquareSize {
		sizes = append(sizes, cursor)
		cursor *= 2
	}
	return sizes
}

// AllSqureSizes returns all of the possible square sizes
func AllSquareSizes() (result []uint64) {
	squareSize := uint64(appconsts.MinSquareSize)
	for squareSize <= appconsts.MaxSquareSize {
		result = append(result, squareSize)
		squareSize = squareSize * 2
	}
	return result
}

// MsgSharesUsed calculates the minimum number of shares a message will take up.
// It accounts for the necessary delimiter and potential padding.
func MsgSharesUsed(msgSize int) int {
	// add the delimiter to the message size
	msgSize = shares.DelimLen(uint64(msgSize)) + msgSize
	shareCount := msgSize / appconsts.SparseShareContentSize
	// increment the share count if the message overflows the last counted share
	if msgSize%appconsts.SparseShareContentSize != 0 {
		shareCount++
	}
	return shareCount
}
