package types

import (
	"github.com/tendermint/tendermint/pkg/consts"
)

// https://github.com/celestiaorg/celestia-app/issues/236
// https://github.com/celestiaorg/celestia-app/issues/239

var allSquareSizes = generateAllSquareSizes()

// generateAllSquareSizes generates and returns all of the possible square sizes
// using the maximum and minimum square sizes
func generateAllSquareSizes() []int {
	sizes := []int{}
	cursor := int(consts.MinSquareSize)
	for cursor <= consts.MaxSquareSize {
		sizes = append(sizes, cursor)
		cursor *= 2
	}
	return sizes
}

// AllSquareSizes calculates all of the square sizes that message could possibly
// fit in
func AllSquareSizes(msgSize int) []uint64 {
	allSizes := allSquareSizes
	fitSizes := []uint64{}
	shareCount := MsgSharesUsed(msgSize)
	for _, size := range allSizes {
		// if the number of shares is larger than that in the square, throw an error
		// note, we use k*k-1 here because at least a single share will be reserved
		// for the transaction paying for the message, therefore the max number of
		// shares a message can be is number of shares in square -1.
		if shareCount > (size*size)-1 {
			continue
		}
		fitSizes = append(fitSizes, uint64(size))
	}
	return fitSizes
}

// MsgSharesUsed calculates the minimum number of shares a message will take up.
// It accounts for the necessary delimiter and potential padding.
func MsgSharesUsed(msgSize int) int {
	// add the delimiter to the message size
	msgSize = DelimLen(uint64(msgSize)) + msgSize
	shareCount := msgSize / consts.MsgShareSize
	// increment the share count if the message overflows the last counted share
	if msgSize%consts.MsgShareSize != 0 {
		shareCount++
	}
	return shareCount
}
