package shares

import (
	"bytes"
	"encoding/binary"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// DelimLen calculates the length of the delimiter for a given unit size
func DelimLen(size uint64) int {
	lenBuf := make([]byte, binary.MaxVarintLen64)
	return binary.PutUvarint(lenBuf, size)
}

// RawTxSize returns the raw tx size that can be used to construct a
// tx of desiredSize bytes. This function is useful in tests to account for
// the length delimiter that is prefixed to a tx when it is converted into
// a compact share
func RawTxSize(desiredSize int) int {
	return desiredSize - DelimLen(uint64(desiredSize))
}

// zeroPadIfNecessary pads the share with trailing zero bytes if the provided
// share has fewer bytes than width. Returns the share unmodified if the
// len(share) is greater than or equal to width.
func zeroPadIfNecessary(share []byte, width int) (padded []byte, bytesOfPadding int) {
	oldLen := len(share)
	if oldLen >= width {
		return share, 0
	}

	missingBytes := width - oldLen
	padByte := []byte{0}
	padding := bytes.Repeat(padByte, missingBytes)
	share = append(share, padding...)
	return share, missingBytes
}

// ParseDelimiter attempts to parse a varint length delimiter from the input
// provided. It returns the input without the len delimiter bytes, the length
// parsed from the varint optionally an error. Unit length delimiters are used
// in compact shares where units (i.e. a transaction) are prefixed with a length
// delimiter that is encoded as a varint. Input should not contain the namespace
// ID or info byte of a share.
func ParseDelimiter(input []byte) (inputWithoutLenDelimiter []byte, unitLen uint64, err error) {
	if len(input) == 0 {
		return input, 0, nil
	}

	l := binary.MaxVarintLen64
	if len(input) < binary.MaxVarintLen64 {
		l = len(input)
	}

	delimiter, _ := zeroPadIfNecessary(input[:l], binary.MaxVarintLen64)

	// read the length of the data
	r := bytes.NewBuffer(delimiter)
	dataLen, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, 0, err
	}

	// calculate the number of bytes used by the delimiter
	lenBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(lenBuf, dataLen)

	// return the input without the length delimiter
	return input[n:], dataLen, nil
}

// AvailableBytesFromCompactShares returns the maximum amount of bytes that could fit in `n` compact shares.
// Note that all compact shares are length prefixed. To account for this use `RawTxSize`.
func AvailableBytesFromCompactShares(n int) int {
	if n <= 0 {
		return 0
	}
	if n == 1 {
		return appconsts.FirstCompactShareContentSize
	}
	return (n-1)*appconsts.ContinuationCompactShareContentSize + appconsts.FirstCompactShareContentSize
}

// AvailableBytesFromSparseShares returns the maximum amount of bytes that could fit in `n` sparse shares
func AvailableBytesFromSparseShares(n int) int {
	if n <= 0 {
		return 0
	}
	if n == 1 {
		return appconsts.FirstSparseShareContentSize
	}
	return (n-1)*appconsts.ContinuationSparseShareContentSize + appconsts.FirstSparseShareContentSize
}
