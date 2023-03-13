package shares

import (
	"math"
)

// FitsInSquare uses the non interactive default rules to see if blobs of
// some lengths will fit in a square of squareSize starting at share index
// cursor. Returns whether the blobs fit in the square and the number of
// shares used by blobs. See non-interactive default rules
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#non-interactive-default-rules
// https://github.com/celestiaorg/celestia-app/blob/1b80b94a62c8c292f569e2fc576e26299985681a/docs/architecture/adr-009-non-interactive-default-rules-for-reduced-padding.md
func FitsInSquare(cursor, squareSize int, blobShareLens ...int) (bool, int) {
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
	cursor, _ = NextMultipleOfBlobMinSquareSize(cursor, firstBlobLen, squareSize)
	sharesUsed, _ := BlobSharesUsedNonInteractiveDefaults(cursor, squareSize, blobShareLens...)
	return cursor+sharesUsed <= squareSize*squareSize, sharesUsed
}

// BlobSharesUsedNonInteractiveDefaults returns the number of shares used by a given set
// of blobs share lengths. It follows the non-interactive default rules and
// returns the share indexes for each blob.
func BlobSharesUsedNonInteractiveDefaults(cursor, squareSize int, blobShareLens ...int) (sharesUsed int, indexes []uint32) {
	start := cursor
	indexes = make([]uint32, len(blobShareLens))
	for i, blobLen := range blobShareLens {
		cursor, _ = NextMultipleOfBlobMinSquareSize(cursor, blobLen, squareSize)
		indexes[i] = uint32(cursor)
		cursor += blobLen
	}
	return cursor - start, indexes
}

// NextMultipleOfBlobMinSquareSize determines the next index in a square that is
// a multiple of the blob's minimum square size. This function returns false if
// the entire the blob cannot fit on the given row. Assumes that all args are
// non negative, and that squareSize is a power of two.
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#non-interactive-default-rules
// https://github.com/celestiaorg/celestia-app/blob/1b80b94a62c8c292f569e2fc576e26299985681a/docs/architecture/adr-009-non-interactive-default-rules-for-reduced-padding.md
func NextMultipleOfBlobMinSquareSize(cursor, blobLen, squareSize int) (index int, fitsInRow bool) {
	// if we're starting at the beginning of the row, then return as there are
	// no cases where we don't start at 0.
	if isStartOfRow(cursor, squareSize) {
		return cursor, true
	}

	blobMinSquareSize := MinSquareSize(blobLen)
	startOfNextRow := ((cursor / squareSize) + 1) * squareSize
	cursor = roundUpBy(cursor, blobMinSquareSize)
	switch {
	// the entire blob fits in this row
	case cursor+blobLen <= startOfNextRow:
		return cursor, true
	// only a portion of the blob fits in this row
	case cursor+blobMinSquareSize <= startOfNextRow:
		return cursor, false
	// none of the blob fits on this row, so return the start of the next row
	default:
		return startOfNextRow, false
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

// MinSquareSize returns the minimum square size that can contain shareCount
// number of shares.
func MinSquareSize(shareCount int) int {
	return RoundUpPowerOfTwo(int(math.Ceil(math.Sqrt(float64(shareCount)))))
}

// isStartOfRow returns true if cursor is at the start of a row
func isStartOfRow(cursor, squareSize int) bool {
	return cursor == 0 || cursor%squareSize == 0
}
