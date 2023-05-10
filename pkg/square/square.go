package square

import (
	"bytes"
	"fmt"
	"math"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	core "github.com/tendermint/tendermint/types"
)

// Build takes an arbitrary long list of (prioritized) transactions and builds a square that is never
// greater than maxSquareSize. It also returns the ordered list of transactions that are present
// in the square and which have all PFBs trailing regular transactions. Note, this function does
// not check the underlying validity of the transactions.
// Errors should not occur and would reflect a violation in an invariant.
func Build(txs [][]byte, maxSquareSize int) (Square, [][]byte, error) {
	builder, err := NewBuilder(maxSquareSize)
	if err != nil {
		return nil, nil, err
	}
	normalTxs := make([][]byte, 0, len(txs))
	blobTxs := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		blobTx, isBlobTx := core.UnmarshalBlobTx(tx)
		if isBlobTx {
			if builder.AppendBlobTx(blobTx) {
				blobTxs = append(blobTxs, tx)
			}
		} else {
			if builder.AppendTx(tx) {
				normalTxs = append(normalTxs, tx)
			}
		}
	}
	square, err := builder.Export()
	return square, append(normalTxs, blobTxs...), err
}

// Construct takes the exact list of ordered transactions and constructs a square, validating that
//   - all PFBs are ordered after regular transactions that
//   - the transactions don't collectively exceed the maxSquareSize.
//
// Note that this function does not check the underlying validity of
// the transactions.
func Construct(txs [][]byte, maxSquareSize int) (Square, error) {
	builder, err := NewBuilder(maxSquareSize, txs...)
	if err != nil {
		return nil, err
	}
	return builder.Export()
}

// TxShareRange returns the range of share indexes that the tx, specified by txIndex, occupies.
// Both ends of the range are inclusive.
func TxShareRange(txs [][]byte, txIndex int) (shares.ShareRange, error) {
	builder, err := NewBuilder(appconsts.DefaultMaxSquareSize, txs...)
	if err != nil {
		return shares.ShareRange{}, err
	}

	return builder.FindTxShareRange(txIndex)
}

// BlobShareRange returns the range of share indexes that the blob, identified by txIndex and blobIndex, occupies.
// Both ends of the range are inclusive.
func BlobShareRange(txs [][]byte, txIndex, blobIndex int) (shares.ShareRange, error) {
	builder, err := NewBuilder(appconsts.DefaultMaxSquareSize, txs...)
	if err != nil {
		return shares.ShareRange{}, err
	}

	start, err := builder.FindBlobStartingIndex(txIndex, blobIndex)
	if err != nil {
		return shares.ShareRange{}, err
	}

	blobLen, err := builder.BlobShareLength(txIndex, blobIndex)
	if err != nil {
		return shares.ShareRange{}, err
	}

	return shares.ShareRange{Start: int(start), End: int(start) + blobLen - 1}, nil
}

// Square is a 2D square of shares with symmetrical sides that are always a power of 2.
type Square []shares.Share

// Size returns the size of the sides of a square
func (s Square) Size() uint64 {
	return Size(len(s))
}

func Size(len int) uint64 {
	return uint64(math.Sqrt(float64(len)))
}

// Equals returns true if two squares are equal
func (s Square) Equals(other Square) bool {
	if len(s) != len(other) {
		return false
	}
	for i := range s {
		if !bytes.Equal(s[i].ToBytes(), other[i].ToBytes()) {
			return false
		}
	}
	return true
}

// EmptySquare returns a 1x1 square with a single tail padding share
func EmptySquare() Square {
	return shares.TailPaddingShares(appconsts.MinShareCount)
}

func writeSquare(
	txWriter, pfbWriter *shares.CompactShareSplitter,
	blobWriter *shares.SparseShareSplitter,
	nonReservedStart, squareSize int,
) (Square, error) {
	totalShares := squareSize * squareSize
	pfbStartIndex := txWriter.Count()
	paddingStartIndex := pfbStartIndex + pfbWriter.Count()
	if nonReservedStart < paddingStartIndex {
		return nil, fmt.Errorf("nonReservedStart %d is too small to fit all PFBs and txs", nonReservedStart)
	}
	padding := shares.ReservedPaddingShares(nonReservedStart - paddingStartIndex)
	endOfLastBlob := nonReservedStart + blobWriter.Count()
	if totalShares < endOfLastBlob {
		return nil, fmt.Errorf("square size %d is too small to fit all blobs", totalShares)
	}

	txShares, err := txWriter.Export()
	if err != nil {
		return nil, fmt.Errorf("failed to export tx shares: %w", err)
	}

	pfbShares, err := pfbWriter.Export()
	if err != nil {
		return nil, fmt.Errorf("failed to export pfb shares: %w", err)
	}

	square := make([]shares.Share, totalShares)
	copy(square[:], txShares)
	copy(square[pfbStartIndex:], pfbShares)
	if blobWriter.Count() > 0 {
		copy(square[paddingStartIndex:], padding)
		copy(square[nonReservedStart:], blobWriter.Export())
	}
	if totalShares > endOfLastBlob {
		copy(square[endOfLastBlob:], shares.TailPaddingShares(totalShares-endOfLastBlob))
	}

	return square, nil
}
