package malicious

import (
	"bytes"
	"fmt"
	"sort"

	"google.golang.org/protobuf/proto"

	"github.com/celestiaorg/go-square/v2"
	"github.com/celestiaorg/go-square/v2/inclusion"
	"github.com/celestiaorg/go-square/v2/share"
	blobtx "github.com/celestiaorg/go-square/v2/tx"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

type ExportFn func(builder *square.Builder) (square.Square, error)

// Build takes an arbitrarily long list of (prioritized) transactions and builds a square that is never
// greater than maxSquareSize. It also returns the ordered list of transactions that are present
// in the square and which have all PFBs trailing regular transactions. Note, this function does
// not check the underlying validity of the transactions.
// Errors should not occur and would reflect a violation in an invariant.
func Build(txs [][]byte, _ uint64, maxSquareSize int, efn ExportFn) (square.Square, [][]byte, error) {
	builder, err := square.NewBuilder(maxSquareSize, appconsts.SubtreeRootThreshold)
	if err != nil {
		return nil, nil, err
	}
	normalTxs := make([][]byte, 0, len(txs))
	blobTxs := make([][]byte, 0, len(txs))
	for idx, tx := range txs {
		blobTx, isBlobTx, err := blobtx.UnmarshalBlobTx(tx)
		if isBlobTx {
			if err != nil {
				return nil, nil, fmt.Errorf("unmarshaling blob tx %d: %w", idx, err)
			}
			if builder.AppendBlobTx(blobTx) {
				blobTxs = append(blobTxs, tx)
			}
		} else if builder.AppendTx(tx) {
			normalTxs = append(normalTxs, tx)
		}
	}
	// note that this is using the malicious Export function
	square, err := efn(builder)
	return square, append(normalTxs, blobTxs...), err
}

// Construct takes the exact list of ordered transactions and constructs a
// square. This mimics the functionality of the normal Construct function, but
// acts maliciously by not following some of the block validity rules.
func Construct(txs [][]byte, _ uint64, maxSquareSize int, efn ExportFn) (square.Square, error) {
	builder, err := square.NewBuilder(maxSquareSize, appconsts.SubtreeRootThreshold, txs...)
	if err != nil {
		return nil, err
	}
	return efn(builder)
}

var _ ExportFn = OutOfOrderExport

// OutOfOrderExport constructs the square in a malicious and deterministic way
// by swapping the first two blobs with different namespaces.
func OutOfOrderExport(b *square.Builder) (square.Square, error) {
	// if there are no transactions, return an empty square
	if b.IsEmpty() {
		return square.EmptySquare(), nil
	}

	// calculate the square size.
	// NOTE: A future optimization could be to recalculate the currentSize based on the actual
	// interblob padding used when the blobs are correctly ordered instead of using worst case padding.
	ss := inclusion.BlobMinSquareSize(b.CurrentSize())

	// sort the blobs in order of namespace. We use slice stable here to respect the
	// order of multiple blobs within a namespace as per the priority of the PFB
	sort.SliceStable(b.Blobs, func(i, j int) bool {
		return bytes.Compare(b.Blobs[i].Blob.Namespace().Bytes(), b.Blobs[j].Blob.Namespace().Bytes()) < 0
	})

	if len(b.Blobs) > 1 {
		// iterate through each blob and find the first two that have different
		// namespaces and swap them.
		for i := 0; i < len(b.Blobs)-1; i++ {
			if !bytes.Equal(b.Blobs[i].Blob.Namespace().Bytes(), b.Blobs[i+1].Blob.Namespace().Bytes()) {
				b.Blobs[i], b.Blobs[i+1] = b.Blobs[i+1], b.Blobs[i]
				break
			}
		}
	}

	// write all the regular transactions into compact shares
	txWriter := share.NewCompactShareSplitter(share.TxNamespace, share.ShareVersionZero)
	for _, tx := range b.Txs {
		if err := txWriter.WriteTx(tx); err != nil {
			return nil, fmt.Errorf("writing tx into compact shares: %w", err)
		}
	}

	// begin to iteratively add blobs to the sparse share splitter calculating the actual padding
	nonReservedStart := b.TxCounter.Size() + b.PfbCounter.Size()
	cursor := nonReservedStart
	endOfLastBlob := nonReservedStart
	blobWriter := share.NewSparseShareSplitter()
	for i, element := range b.Blobs {
		// NextShareIndex returned where the next blob should start so as to comply with the share commitment rules
		// We fill out the remaining
		cursor = inclusion.NextShareIndex(cursor, element.NumShares, b.SubtreeRootThreshold())
		if i == 0 {
			nonReservedStart = cursor
		}

		// defensively check that the actual padding never exceeds the max padding initially allocated for it
		padding := cursor - endOfLastBlob
		if padding > element.MaxPadding {
			return nil, fmt.Errorf("blob has %d padding shares, but %d was the max possible", padding, element.MaxPadding)
		}

		// record the starting share index of the blob in the PFB that paid for it
		b.Pfbs[element.PfbIndex].ShareIndexes[element.BlobIndex] = uint32(cursor)
		// If this is not the first blob, we add padding by writing padded shares to the previous blob
		// (which could be of a different namespace)
		if i > 0 {
			if err := blobWriter.WriteNamespacePaddingShares(padding); err != nil {
				return nil, fmt.Errorf("writing padding into sparse shares: %w", err)
			}
		}
		// Finally write the blob itself
		if err := blobWriter.Write(element.Blob); err != nil {
			return nil, fmt.Errorf("writing blob into sparse shares: %w", err)
		}
		// increment the cursor by the size of the blob
		cursor += element.NumShares
		endOfLastBlob = cursor
	}

	// write all the pay for blob transactions into compact shares. We need to do this after allocating the blobs to their
	// appropriate shares as the starting index of each blob needs to be included in the PFB transaction
	pfbWriter := share.NewCompactShareSplitter(share.PayForBlobNamespace, share.ShareVersionZero)
	for _, iw := range b.Pfbs {
		iwBytes, err := proto.Marshal(iw)
		if err != nil {
			return nil, fmt.Errorf("marshaling pay for blob tx: %w", err)
		}
		if err := pfbWriter.WriteTx(iwBytes); err != nil {
			return nil, fmt.Errorf("writing pay for blob tx into compact shares: %w", err)
		}
	}

	// defensively check that the counter is always greater in share count than the pfbTxWriter.
	if b.PfbCounter.Size() < pfbWriter.Count() {
		return nil, fmt.Errorf("pfbCounter.Size() < pfbTxWriter.Count(): %d < %d", b.PfbCounter.Size(), pfbWriter.Count())
	}

	// Write out the square
	square, err := square.WriteSquare(txWriter, pfbWriter, blobWriter, nonReservedStart, ss)
	if err != nil {
		return nil, fmt.Errorf("writing square: %w", err)
	}

	return square, nil
}
