package square

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/tendermint/tendermint/pkg/consts"
	coretypes "github.com/tendermint/tendermint/proto/tendermint/types"
	core "github.com/tendermint/tendermint/types"
)

type Builder struct {
	// maxCapacity is an upper bound on the total amount of shares that could fit in the biggest square
	maxCapacity int
	// currentSize is an overestimate for the number of shares used by this builder.
	currentSize int

	// here we keep track of the actual data to go in a square
	txs   [][]byte
	pfbs  []*coretypes.IndexWrapper
	blobs []*element

	// for compact shares we use a counter to track the amount of shares needed
	txCounter  *shares.CompactShareCounter
	pfbCounter *shares.CompactShareCounter
}

func NewBuilder(maxSquareSize int) (*Builder, error) {
	if maxSquareSize < 0 {
		return nil, errors.New("max square size must be positive")
	}
	if !shares.IsPowerOfTwo(maxSquareSize) {
		return nil, errors.New("max square size must be a power of two")
	}
	return &Builder{
		maxCapacity: maxSquareSize * maxSquareSize,
		blobs:       make([]*element, 0),
		pfbs:        make([]*coretypes.IndexWrapper, 0),
		txs:         make([][]byte, 0),
		txCounter:   shares.NewCompactShareCounter(),
		pfbCounter:  shares.NewCompactShareCounter(),
	}, nil
}

// AppendTx attempts to allocate the transaction to the square. It returns false if there is not
// enough space in the square to fit the transaction.
func (c *Builder) AppendTx(tx []byte) bool {
	lenChange := c.txCounter.Add(len(tx))
	if c.canFit(lenChange) {
		c.txs = append(c.txs, tx)
		c.currentSize += lenChange
		return true
	}
	c.txCounter.Revert()
	return false
}

// AppendBlobTx attempts to allocate the blob transaction to the square. It returns false if there is not
// enough space in the square to fit the transaction.
func (c *Builder) AppendBlobTx(blobTx coretypes.BlobTx) (bool, error) {
	iw := &coretypes.IndexWrapper{
		Tx:           blobTx.Tx,
		TypeId:       consts.ProtoIndexWrapperTypeID,
		ShareIndexes: worstCaseShareIndexes(len(blobTx.Blobs), c.maxCapacity),
	}
	size := iw.Size()
	pfbShareDiff := c.pfbCounter.Add(size)

	// create a new blob element for each blob and track the worst-case share count
	blobElements := make([]*element, len(blobTx.Blobs))
	maxBlobShareCount := 0
	for idx, blobProto := range blobTx.Blobs {
		blob, err := types.BlobFromProto(blobProto)
		if err != nil {
			return false, err
		}
		blobElements[idx] = newElement(blob, len(c.pfbs), idx)
		maxBlobShareCount += blobElements[idx].maxShareOffset()
	}

	if c.canFit(pfbShareDiff + maxBlobShareCount) {
		c.blobs = append(c.blobs, blobElements...)
		c.pfbs = append(c.pfbs, iw)
		c.currentSize += (pfbShareDiff + maxBlobShareCount)
		return true, nil
	}
	c.pfbCounter.Revert()
	return false, nil
}

// Export returns the square.
func (c *Builder) Export() (Square, error) {
	// if there are no transactions, return an empty square
	if c.isEmpty() {
		return EmptySquare(), nil
	}

	// calculate the square size.
	// NOTE: A future optimization could be to recalculate the currentSize based on the actual
	// interblob padding used when the blobs are correctly ordered instead of using worst case padding.
	ss := shares.BlobMinSquareSize(c.currentSize)

	// sort the blobs in order of namespace. We use slice stable here to respect the
	// order of multiple blobs within a namespace as per the priority of the PFB
	sort.SliceStable(c.blobs, func(i, j int) bool {
		return bytes.Compare(c.blobs[i].blob.NamespaceID, c.blobs[j].blob.NamespaceID) < 0
	})

	// write all the regular transactions into compact shares
	txWriter := shares.NewCompactShareSplitter(namespace.TxNamespace, appconsts.ShareVersionZero)
	for _, tx := range c.txs {
		if err := txWriter.WriteTx(tx); err != nil {
			return nil, fmt.Errorf("writing tx into compact shares: %w", err)
		}
	}

	// begin to iteratively add blobs to the sparse share splitter calculating the actual padding
	nonReservedStart := c.txCounter.Size() + c.pfbCounter.Size()
	cursor := nonReservedStart
	endOfLastBlob := nonReservedStart
	blobWriter := shares.NewSparseShareSplitter()
	for i, element := range c.blobs {
		// NextShareIndex returned where the next blob should start so as to comply with the share commitment rules
		// We fill out the remaining
		cursor, _ = shares.NextShareIndex(cursor, element.numShares, ss)
		if i == 0 {
			nonReservedStart = cursor
		}

		// defensively check that the actual padding never exceeds the max padding initially allocated for it
		padding := cursor - endOfLastBlob
		if padding > element.maxPadding {
			return nil, fmt.Errorf("blob has %d padding shares, but %d was the max possible", padding, element.maxPadding)
		}

		// record the starting share index of the blob in the PFB that paid for it
		c.pfbs[element.pfbIndex].ShareIndexes[element.blobIndex] = uint32(cursor)
		// If this is not the first blob, we add padding by writing padded shares to the previous blob
		// (which could be of a different namespace)
		if i > 0 {
			if err := blobWriter.WriteNamespacedPaddedShares(padding); err != nil {
				return nil, fmt.Errorf("writing padding into sparse shares: %w", err)
			}
		}
		// Finally write the blob itself
		if err := blobWriter.Write(element.blob); err != nil {
			return nil, fmt.Errorf("writing blob into sparse shares: %w", err)
		}
		// increment the cursor by the size of the blob
		cursor += element.numShares
		endOfLastBlob = cursor
	}

	// write all the pay for blob transactions into compact shares. We need to do this after allocating the blobs to their
	// appropriate shares as the starting index of each blob needs to be included in the PFB transaction
	pfbTxWriter := shares.NewCompactShareSplitter(namespace.PayForBlobNamespace, appconsts.ShareVersionZero)
	for _, iw := range c.pfbs {
		iwBytes, err := iw.Marshal()
		if err != nil {
			return nil, fmt.Errorf("marshaling pay for blob tx: %w", err)
		}
		if err := pfbTxWriter.WriteTx(iwBytes); err != nil {
			return nil, fmt.Errorf("writing pay for blob tx into compact shares: %w", err)
		}
	}

	// defensively check that the counter is always greater in share count than the pfbTxWriter.
	if c.pfbCounter.Size() < pfbTxWriter.Count() {
		return nil, fmt.Errorf("pfbCounter.Size() < pfbTxWriter.Count(): %d < %d", c.pfbCounter.Size(), pfbTxWriter.Count())
	}

	// Write out the square
	return WriteSquare(txWriter, pfbTxWriter, blobWriter, nonReservedStart, ss)
}

func (c *Builder) canFit(shareNum int) bool {
	return c.currentSize+shareNum <= c.maxCapacity
}

func (c *Builder) isEmpty() bool {
	return c.txCounter.Size() == 0 && c.pfbCounter.Size() == 0
}

type element struct {
	blob       core.Blob
	pfbIndex   int
	blobIndex  int
	numShares  int
	maxPadding int
}

func newElement(blob core.Blob, pfbIndex, blobIndex int) *element {
	numShares := shares.SparseSharesNeeded(uint32(len(blob.Data)))
	return &element{
		blob:      blob,
		pfbIndex:  pfbIndex,
		blobIndex: blobIndex,
		numShares: numShares,
		//
		// For cacluating the maximum possible padding consider the following tree
		// where each leaf corresponds to a share.
		//
		//	Depth       Position
		//	0              0
		//	              / \
		//	             /   \
		//	1           0     1
		//	           /\     /\
		//	2         0  1   2  3
		//	         /\  /\ /\  /\
		//	3       0 1 2 3 4 5 6 7
		//
		// Imagine if, according to the share commitment rules, a transcation took up 11 shares
		// and had the merkle mountain tree commitment of 4,4,2,1. The first part of the share commitment
		// would then be something that spans 4 shares and has a depth of 1. The worst case padding
		// would be if the last transaction had a share at leaf index 0. Thus three padding shares would
		// be needed to start the transaction at index 4 and be aligned with the first commitment.
		// Thus the rule is to take the subtreewidh of the share size and subtract 1.
		//
		// Note that the padding would actually belong to the namespace of the transaction before it, but
		// this makes no difference to the total share size.
		maxPadding: shares.SubTreeWidth(numShares) - 1,
	}
}

func (e element) maxShareOffset() int {
	return e.numShares + e.maxPadding
}

func worstCaseShareIndexes(blobs, maxSquareCapacity int) []uint32 {
	shareIndexes := make([]uint32, blobs)
	for i := range shareIndexes {
		shareIndexes[i] = uint32(maxSquareCapacity)
	}
	return shareIndexes
}
