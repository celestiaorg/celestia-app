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

// AllSquareSizes calculates all of the square sizes that blob could possibly
// fit in
func AllSquareSizes(blobSize int) []uint64 {
	allSizes := allSquareSizes
	fitSizes := []uint64{}
	shareCount := BlobSharesUsed(blobSize)
	for _, squareSize := range allSizes {
		// continue if the number of shares is larger than the max number of
		// shares for a blob. At least one share will be occupied by the
		// transaction that pays for this blob. According to the non-interactive
		// default rules, a blob that spans multiple rows must start in a new
		// row. Therefore the blob must start at the second row and may occupy
		// all (squareSize - 1) rows.
		maxNumSharesForBlob := squareSize * (squareSize - 1)
		if shareCount > maxNumSharesForBlob {
			continue
		}
		fitSizes = append(fitSizes, uint64(squareSize))
	}
	return fitSizes
}

// BlobSharesUsed calculates the minimum number of shares a blob will take up.
// It accounts for the necessary delimiter and potential padding.
func BlobSharesUsed(blobSize int) int {
	// add the delimiter to the blob size
	blobSize = shares.DelimLen(uint64(blobSize)) + blobSize
	shareCount := blobSize / appconsts.SparseShareContentSize
	// increment the share count if the blob overflows the last counted share
	if blobSize%appconsts.SparseShareContentSize != 0 {
		shareCount++
	}
	return shareCount
}
