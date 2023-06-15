package shares

import (
	"math"

	"golang.org/x/exp/constraints"
)

// FitsInSquare uses the non interactive default rules to see if blobs of
// some lengths will fit in a square of squareSize starting at share index
// cursor. Returns whether the blobs fit in the square and the number of
// shares used by blobs. See non-interactive default rules
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#non-interactive-default-rules
// https://github.com/celestiaorg/celestia-app/blob/1b80b94a62c8c292f569e2fc576e26299985681a/docs/architecture/adr-009-non-interactive-default-rules-for-reduced-padding.md
func FitsInSquare(cursor, squareSize, subtreeRootThreshold int, blobShareLens ...int) (bool, int) {
	if len(blobShareLens) == 0 {
		if cursor <= squareSize*squareSize {
			return true, 0
		}
		return false, 0
	}
	firstBlobLen := 1
	if len(blobShareLens) > 0 {
		firstBlobLen = blobShareLens[0]
	}
	// here we account for padding between the compact and sparse shares
	cursor = NextShareIndex(cursor, firstBlobLen, squareSize, subtreeRootThreshold)
	sharesUsed, _ := BlobSharesUsedNonInteractiveDefaults(cursor, squareSize, subtreeRootThreshold, blobShareLens...)
	return cursor+sharesUsed <= squareSize*squareSize, sharesUsed
}

// BlobSharesUsedNonInteractiveDefaults returns the number of shares used by a given set
// of blobs share lengths. It follows the non-interactive default rules and
// returns the share indexes for each blob.
func BlobSharesUsedNonInteractiveDefaults(cursor, squareSize, subtreeRootThreshold int, blobShareLens ...int) (sharesUsed int, indexes []uint32) {
	start := cursor
	indexes = make([]uint32, len(blobShareLens))
	for i, blobLen := range blobShareLens {
		cursor = NextShareIndex(cursor, blobLen, squareSize, subtreeRootThreshold)
		indexes[i] = uint32(cursor)
		cursor += blobLen
	}
	return cursor - start, indexes
}

// NextShareIndex determines the next index in a square that can be used. It
// follows the non-interactive default rules defined in ADR013. Assumes
// that all args are non negative, and that squareSize is a power of two.
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#non-interactive-default-rules
// https://github.com/celestiaorg/celestia-app/blob/0334749a9e9b989fa0a42b7f011f4a79af8f61aa/docs/architecture/adr-013-non-interactive-default-rules-for-zero-padding.md
func NextShareIndex(cursor, blobShareLen, squareSize, subtreeRootThreshold int) int {
	// if we're starting at the beginning of the row, then return as there are
	// no cases where we don't start at 0.
	if isStartOfRow(cursor, squareSize) {
		return cursor
	}

	treeWidth := SubTreeWidth(blobShareLen, subtreeRootThreshold)
	startOfNextRow := getStartOfNextRow(cursor, squareSize)
	cursor = roundUpBy(cursor, treeWidth)
	switch {
	// the entire blob fits in this row
	case cursor+blobShareLen <= startOfNextRow:
		return cursor
	// only a portion of the blob fits in this row
	case cursor+treeWidth <= startOfNextRow:
		return cursor
	// none of the blob fits on this row, so return the start of the next row
	default:
		return startOfNextRow
	}
}

// roundUpBy rounds cursor up to the next multiple of v. If cursor is divisible
// by v, then it returns cursor
func roundUpBy(cursor, v int) int {
	switch {
	case cursor == 0:
		return cursor
	case cursor%v == 0:
		return cursor
	default:
		return ((cursor / v) + 1) * v
	}
}

// BlobMinSquareSize returns the minimum square size that can contain shareCount
// number of shares.
func BlobMinSquareSize(shareCount int) int {
	return RoundUpPowerOfTwo(int(math.Ceil(math.Sqrt(float64(shareCount)))))
}

// SubTreeWidth determines the maximum number of leaves per subtree in the share
// commitment over a given blob. The input should be the total number of shares
// used by that blob. The reasoning behind this algorithm is discussed in depth
// in ADR013
// (celestia-app/docs/architecture/adr-013-non-interative-default-rules-for-zero-padding).
func SubTreeWidth(shareCount, subtreeRootThreshold int) int {
	// per ADR013, we use a predetermined threshold to determine width of sub
	// trees used to create share commitments
	s := (shareCount / subtreeRootThreshold)

	// round up if the width is not an exact multiple of the threshold
	if shareCount%subtreeRootThreshold != 0 {
		s++
	}

	// use a power of two equal to or larger than the multiple of the subtree
	// root threshold
	s = RoundUpPowerOfTwo(s)

	// use the minimum of the subtree width and the min square size, this
	// gurarantees that a valid value is returned
	return min(s, BlobMinSquareSize(shareCount))
}

func min[T constraints.Integer](i, j T) T {
	if i < j {
		return i
	}
	return j
}

// isStartOfRow returns true if cursor is at the start of a row
func isStartOfRow(cursor, squareSize int) bool {
	return cursor == 0 || cursor%squareSize == 0
}

// getStartOfRow returns the index of the first share in the next row
func getStartOfNextRow(cursor, squareSize int) int {
	return ((cursor / squareSize) + 1) * squareSize
}
