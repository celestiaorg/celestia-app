package shares

// FitsInSquare uses the non interactive default rules to see if blobs of
// some lengths will fit in a square of squareSize starting at share index
// cursor. Returns whether the blobs fit in the square and the number of
// shares used by blobs. See non-interactive default rules
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#non-interactive-default-rules
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
	cursor, _ = NextAlignedPowerOfTwo(cursor, firstBlobLen, squareSize)
	sharesUsed, _ := BlobSharesUsedNonInteractiveDefaults(cursor, squareSize, blobShareLens...)
	return cursor+sharesUsed <= squareSize*squareSize, sharesUsed
}

// BlobSharesUsedNonInteractiveDefaults calculates the number of shares used by a given set
// of blobs share lengths. It follows the non-interactive default rules and
// returns the share indexes for each blob.
func BlobSharesUsedNonInteractiveDefaults(cursor, squareSize int, blobShareLens ...int) (int, []uint32) {
	start := cursor
	indexes := make([]uint32, len(blobShareLens))
	for i, blobLen := range blobShareLens {
		cursor, _ = NextAlignedPowerOfTwo(cursor, blobLen, squareSize)
		indexes[i] = uint32(cursor)
		cursor += blobLen
	}
	return cursor - start, indexes
}

// NextAlignedPowerOfTwo calculates the next index in a row that is an aligned
// power of two and returns false if the entire the blob cannot fit on the given
// row at the next aligned power of two. An aligned power of two means that the
// largest power of two that fits entirely in the blob or the square size. pls
// see specs for further details. Assumes that cursor < squareSize, all args are
// non negative, and that squareSize is a power of two.
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#non-interactive-default-rules
func NextAlignedPowerOfTwo(cursor, blobLen, squareSize int) (int, bool) {
	// if we're starting at the beginning of the row, then return as there are
	// no cases where we don't start at 0.
	if cursor == 0 || cursor%squareSize == 0 {
		return cursor, true
	}

	nextLowest := RoundDownPowerOfTwo(blobLen)
	endOfCurrentRow := ((cursor / squareSize) + 1) * squareSize
	cursor = roundUpBy(cursor, nextLowest)
	switch {
	// the entire blob fits in this row
	case cursor+blobLen <= endOfCurrentRow:
		return cursor, true
	// only a portion of the blob fits in this row
	case cursor+nextLowest <= endOfCurrentRow:
		return cursor, false
	// none of the blob fits on this row, so return the start of the next row
	default:
		return endOfCurrentRow, false
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
