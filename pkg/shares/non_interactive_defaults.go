package shares

// FitsInSquare uses the non interactive default rules to see if messages of
// some lengths will fit in a square of squareSize starting at share index
// cursor. See non-interactive default rules
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#non-interactive-default-rules
func FitsInSquare(cursor, squareSize int, msgShareLens ...int) (bool, int) {
	// if there are 0 messages and the cursor already fits inside the square,
	// then we already know that everything fits in the square.
	if len(msgShareLens) == 0 && cursor/squareSize <= squareSize {
		return true, 0
	}
	firstMsgLen := 1
	if len(msgShareLens) > 0 {
		firstMsgLen = msgShareLens[0]
	}
	// here we account for padding between the compact and sparse shares
	cursor, _ = NextAlignedPowerOfTwo(cursor, firstMsgLen, squareSize)
	sharesUsed, _ := MsgSharesUsedNonInteractiveDefaults(cursor, squareSize, msgShareLens...)
	return cursor+sharesUsed <= squareSize*squareSize, sharesUsed
}

// MsgSharesUsedNonInteractiveDefaults calculates the number of shares used by a given set
// of messages share lengths. It follows the non-interactive default rules and
// returns the share indexes for each message.
func MsgSharesUsedNonInteractiveDefaults(cursor, squareSize int, msgShareLens ...int) (int, []uint32) {
	start := cursor
	indexes := make([]uint32, len(msgShareLens))
	for i, msgLen := range msgShareLens {
		cursor, _ = NextAlignedPowerOfTwo(cursor, msgLen, squareSize)
		indexes[i] = uint32(cursor)
		cursor += msgLen
	}
	return cursor - start, indexes
}

// NextAlignedPowerOfTwo calculates the next index in a row that is an aligned
// power of two and returns false if the entire the msg cannot fit on the given
// row at the next aligned power of two. An aligned power of two means that the
// largest power of two that fits entirely in the msg or the square size. pls
// see specs for further details. Assumes that cursor < squareSize, all args are
// non negative, and that squareSize is a power of two.
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#non-interactive-default-rules
func NextAlignedPowerOfTwo(cursor, msgLen, squareSize int) (int, bool) {
	// if we're starting at the beginning of the row, then return as there are
	// no cases where we don't start at 0.
	if cursor == 0 || cursor%squareSize == 0 {
		return cursor, true
	}

	nextLowest := nextLowestPowerOfTwo(msgLen)
	endOfCurrentRow := ((cursor / squareSize) + 1) * squareSize
	cursor = roundUpBy(cursor, nextLowest)
	switch {
	// the entire message fits in this row
	case cursor+msgLen <= endOfCurrentRow:
		return cursor, true
	// only a portion of the message fits in this row
	case cursor+nextLowest <= endOfCurrentRow:
		return cursor, false
	// none of the message fits on this row, so return the start of the next row
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

func nextPowerOfTwo(v int) int {
	k := 1
	for k < v {
		k = k << 1
	}
	return k
}

func nextLowestPowerOfTwo(v int) int {
	c := nextPowerOfTwo(v)
	if c == v {
		return c
	}
	return c / 2
}
