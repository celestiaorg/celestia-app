package shares

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
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
	cursor, _ = NextShareIndex(cursor, firstBlobLen, squareSize)
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
		cursor, _ = NextShareIndex(cursor, blobLen, squareSize)
		indexes[i] = uint32(cursor)
		cursor += blobLen
	}
	return cursor - start, indexes
}

// NextShareIndex determines the next index in a square that can be used. It
// follows the non-interactive default rules defined in ADR013. This function
// returns false if the entire the blob cannot fit on the given row. Assumes
// that all args are non negative, and that squareSize is a power of two.
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#non-interactive-default-rules
// https://github.com/celestiaorg/celestia-app/blob/0334749a9e9b989fa0a42b7f011f4a79af8f61aa/docs/architecture/adr-013-non-interactive-default-rules-for-zero-padding.md
func NextShareIndex(cursor, blobShareLen, squareSize int) (index int, fitsInRow bool) {
	// if we're starting at the beginning of the row, then return as there are
	// no cases where we don't start at 0.
	if isStartOfRow(cursor, squareSize) {
		return cursor, true
	}

	blobMinSquareSize := SubTreeWidth(blobShareLen)
	startOfNextRow := ((cursor / squareSize) + 1) * squareSize
	cursor = roundUpBy(cursor, blobMinSquareSize)
	switch {
	// the entire blob fits in this row
	case cursor+blobShareLen <= startOfNextRow:
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

// SubTreeWidth determines how many shares should be included when creating
// subtree. This is used for determining which subtree roots are used in the
// share commitment for PFBs, and therefore also the padding between blobs. The
// reasoning behind this algorithm is discussed in depth in ADR013.
func SubTreeWidth(shareCount int) int {
	// per ADR013, we use a predetermined threshold to determine width of sub
	// trees used to create share commitments
	s := (shareCount / appconsts.SubtreeRootHeightThreshold)

	// round up if the width is not an exact multiple of the threshold
	if shareCount%appconsts.SubtreeRootHeightThreshold != 0 {
		s++
	}

	// use a power of two equal to or larger than the multiple of the threshold
	s = RoundUpPowerOfTwo(s)

	// using a value below the min square size is wasteful, see ADR013 for more
	// details
	if s < appconsts.DefaultMinSquareSize {
		return appconsts.DefaultMinSquareSize
	}

	return s
}

// isStartOfRow returns true if cursor is at the start of a row
func isStartOfRow(cursor, squareSize int) bool {
	return cursor == 0 || cursor%squareSize == 0
}
