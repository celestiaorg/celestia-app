package square

import (
	"bytes"
	"fmt"
	"math"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	blob "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/types"
	coreproto "github.com/tendermint/tendermint/proto/tendermint/types"
	core "github.com/tendermint/tendermint/types"
)

// Build takes an arbitrary long list of (prioritized) transactions and builds a square that is never
// greater than maxSquareSize. It also returns the ordered list of transactions that are present
// in the square and which have all PFBs trailing regular transactions. Note, this function does
// not check the underlying validity of the transactions.
// Errors should not occur and would reflect a violation in an invariant.
func Build(txs [][]byte, appVersion uint64, maxSquareSize int) (Square, [][]byte, error) {
	builder, err := NewBuilder(maxSquareSize, appVersion)
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
//   - all blobTxs are ordered after non-blob transactions
//   - the transactions don't collectively exceed the maxSquareSize.
//
// Note that this function does not check the underlying validity of
// the transactions.
func Construct(txs [][]byte, appVersion uint64, maxSquareSize int) (Square, error) {
	builder, err := NewBuilder(maxSquareSize, appVersion, txs...)
	if err != nil {
		return nil, err
	}
	return builder.Export()
}

// Deconstruct takes a square and returns the ordered list of block
// transactions that constructed that square
//
// This method uses the wrapped pfbs in the PFB namespace to identify and
// decode the blobs. Data that may be included in the square but isn't
// recognised by the square construction algorithm will be ignored
func Deconstruct(s Square, decoder types.TxDecoder) (core.Txs, error) {
	if s.IsEmpty() {
		return []core.Tx{}, nil
	}

	// Work out which range of shares are non-pfb transactions
	// and which ones are pfb transactions
	txShareRange, err := shares.GetShareRangeForNamespace(s, namespace.TxNamespace)
	if err != nil {
		return nil, err
	}
	if txShareRange.Start != 0 {
		return nil, fmt.Errorf("expected txs to start at index 0, but got %d", txShareRange.Start)
	}

	wpfbShareRange, err := shares.GetShareRangeForNamespace(s[txShareRange.End:], namespace.PayForBlobNamespace)
	if err != nil {
		return nil, err
	}

	// If there are no pfb transactions, then we can just return the txs
	if wpfbShareRange.IsEmpty() {
		return shares.ParseTxs(s[txShareRange.Start:txShareRange.End])
	}

	// We expect pfb transactions to come directly after non-pfb transactions
	if wpfbShareRange.Start != 0 {
		return nil, fmt.Errorf("expected PFBs to start directly after non PFBs at index %d, but got %d", txShareRange.End, wpfbShareRange.Start)
	}
	wpfbShareRange.Add(txShareRange.End)

	// Parse both txs
	txs, err := shares.ParseTxs(s[txShareRange.Start:txShareRange.End])
	if err != nil {
		return nil, err
	}

	wpfbs, err := shares.ParseTxs(s[wpfbShareRange.Start:wpfbShareRange.End])
	if err != nil {
		return nil, err
	}

	// loop through the wrapped pfbs and generate the original
	// blobTx that they derive from
	for i, wpfbBytes := range wpfbs {
		wpfb, isWpfb := core.UnmarshalIndexWrapper(wpfbBytes)
		if !isWpfb {
			return nil, fmt.Errorf("expected wrapped PFB at index %d", i)
		}
		if len(wpfb.ShareIndexes) == 0 {
			return nil, fmt.Errorf("wrapped PFB %d has no blobs attached", i)
		}
		pfbTx, err := decoder(wpfb.Tx)
		if err != nil {
			return nil, err
		}
		pfbMsgs := pfbTx.GetMsgs()
		if len(pfbMsgs) != 1 {
			return nil, fmt.Errorf("expected PFB to have 1 message, but got %d", len(pfbMsgs))
		}
		pfb, isPfb := pfbMsgs[0].(*blob.MsgPayForBlobs)
		if !isPfb {
			return nil, fmt.Errorf("expected PFB message, but got %T", pfbMsgs[0])
		}
		if len(pfb.BlobSizes) != len(wpfb.ShareIndexes) {
			return nil, fmt.Errorf("expected PFB to have %d blob sizes, but got %d", len(wpfb.ShareIndexes), len(pfb.BlobSizes))
		}

		blobs := make([]*coreproto.Blob, len(wpfb.ShareIndexes))
		for j, shareIndex := range wpfb.ShareIndexes {
			end := int(shareIndex) + shares.SparseSharesNeeded(pfb.BlobSizes[j])
			parsedBlobs, err := shares.ParseBlobs(s[shareIndex:end])
			if err != nil {
				return nil, err
			}
			if len(parsedBlobs) != 1 {
				return nil, fmt.Errorf("expected to parse a single blob, but got %d", len(blobs))
			}

			blobs[j] = &coreproto.Blob{
				NamespaceId:      parsedBlobs[0].NamespaceID,
				Data:             parsedBlobs[0].Data,
				ShareVersion:     uint32(parsedBlobs[0].ShareVersion),
				NamespaceVersion: uint32(parsedBlobs[0].NamespaceVersion),
			}
		}

		tx, err := core.MarshalBlobTx(wpfb.Tx, blobs...)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}

	return txs, nil
}

// TxShareRange returns the range of share indexes that the tx, specified by txIndex, occupies.
// The range is end exclusive.
func TxShareRange(txs [][]byte, txIndex int, appVersion uint64) (shares.Range, error) {
	builder, err := NewBuilder(appconsts.SquareSizeUpperBound(appVersion), appVersion, txs...)
	if err != nil {
		return shares.Range{}, err
	}

	return builder.FindTxShareRange(txIndex)
}

// BlobShareRange returns the range of share indexes that the blob, identified by txIndex and blobIndex, occupies.
// The range is end exclusive.
func BlobShareRange(txs [][]byte, txIndex, blobIndex int, appVersion uint64) (shares.Range, error) {
	builder, err := NewBuilder(appconsts.SquareSizeUpperBound(appVersion), appVersion, txs...)
	if err != nil {
		return shares.Range{}, err
	}

	start, err := builder.FindBlobStartingIndex(txIndex, blobIndex)
	if err != nil {
		return shares.Range{}, err
	}

	blobLen, err := builder.BlobShareLength(txIndex, blobIndex)
	if err != nil {
		return shares.Range{}, err
	}
	end := start + blobLen

	return shares.NewRange(start, end), nil
}

// Square is a 2D square of shares with symmetrical sides that are always a power of 2.
type Square []shares.Share

// Size returns the size of the sides of a square
func (s Square) Size() int {
	return Size(len(s))
}

func Size(len int) int {
	return shares.RoundUpPowerOfTwo(int(math.Ceil(math.Sqrt(float64(len)))))
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

// WrappedPFBs returns the wrapped PFBs in a square
func (s Square) WrappedPFBs() (core.Txs, error) {
	wpfbShareRange, err := shares.GetShareRangeForNamespace(s, namespace.PayForBlobNamespace)
	if err != nil {
		return core.Txs{}, nil
	}
	return shares.ParseTxs(s[wpfbShareRange.Start:wpfbShareRange.End])
}

func (s Square) IsEmpty() bool {
	return s.Equals(EmptySquare())
}

// EmptySquare returns a 1x1 square with a single tail padding share
func EmptySquare() Square {
	return shares.TailPaddingShares(appconsts.MinShareCount)
}

func WriteSquare(
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
