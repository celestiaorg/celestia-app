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

	// here we keep track of the pending data to go in a square
	txs   [][]byte
	pfbs  []*coretypes.IndexWrapper
	blobs []*element

	// for compact shares we use a counter to track the amount of shares needed
	txCounter  *shares.CompactShareCounter
	pfbCounter *shares.CompactShareCounter

	done                 bool
	subtreeRootThreshold int
}

func NewBuilder(maxSquareSize, subtreeRootThreshold int, txs ...[]byte) (*Builder, error) {
	if maxSquareSize <= 0 {
		return nil, errors.New("max square size must be strictly positive")
	}
	if subtreeRootThreshold <= 0 {
		return nil, errors.New("subtreeRootThreshold must be strictly positive")
	}
	if !shares.IsPowerOfTwo(maxSquareSize) {
		return nil, errors.New("max square size must be a power of two")
	}
	builder := &Builder{
		maxCapacity:          maxSquareSize * maxSquareSize,
		subtreeRootThreshold: subtreeRootThreshold,
		blobs:                make([]*element, 0),
		pfbs:                 make([]*coretypes.IndexWrapper, 0),
		txs:                  make([][]byte, 0),
		txCounter:            shares.NewCompactShareCounter(),
		pfbCounter:           shares.NewCompactShareCounter(),
	}
	seenFirstBlobTx := false
	for idx, tx := range txs {
		blobTx, isBlobTx := core.UnmarshalBlobTx(tx)
		if isBlobTx {
			seenFirstBlobTx = true
			if !builder.AppendBlobTx(blobTx) {
				return nil, fmt.Errorf("not enough space to append blob tx at index %d", idx)
			}
		} else {
			if seenFirstBlobTx {
				return nil, fmt.Errorf("normal tx at index %d can not be appended after blob tx", idx)
			}
			if !builder.AppendTx(tx) {
				return nil, fmt.Errorf("not enough space to append tx at index %d", idx)
			}
		}
	}
	return builder, nil
}

// AppendTx attempts to allocate the transaction to the square. It returns false if there is not
// enough space in the square to fit the transaction.
func (b *Builder) AppendTx(tx []byte) bool {
	lenChange := b.txCounter.Add(len(tx))
	if b.canFit(lenChange) {
		b.txs = append(b.txs, tx)
		b.currentSize += lenChange
		b.done = false
		return true
	}
	b.txCounter.Revert()
	return false
}

// AppendBlobTx attempts to allocate the blob transaction to the square. It returns false if there is not
// enough space in the square to fit the transaction.
func (b *Builder) AppendBlobTx(blobTx coretypes.BlobTx) bool {
	iw := &coretypes.IndexWrapper{
		Tx:           blobTx.Tx,
		TypeId:       consts.ProtoIndexWrapperTypeID,
		ShareIndexes: worstCaseShareIndexes(len(blobTx.Blobs), b.maxCapacity),
	}
	size := iw.Size()
	pfbShareDiff := b.pfbCounter.Add(size)

	// create a new blob element for each blob and track the worst-case share count
	blobElements := make([]*element, len(blobTx.Blobs))
	maxBlobShareCount := 0
	for idx, blobProto := range blobTx.Blobs {
		blob, err := types.BlobFromProto(blobProto)
		if err != nil {
			return false
		}
		blobElements[idx] = newElement(blob, len(b.pfbs), idx, b.subtreeRootThreshold)
		maxBlobShareCount += blobElements[idx].maxShareOffset()
	}

	if b.canFit(pfbShareDiff + maxBlobShareCount) {
		b.blobs = append(b.blobs, blobElements...)
		b.pfbs = append(b.pfbs, iw)
		b.currentSize += (pfbShareDiff + maxBlobShareCount)
		b.done = false
		return true
	}
	b.pfbCounter.Revert()
	return false
}

// Export constructs the square.
func (b *Builder) Export() (Square, error) {
	// if there are no transactions, return an empty square
	if b.isEmpty() {
		return EmptySquare(), nil
	}

	// calculate the square size.
	// NOTE: A future optimization could be to recalculate the currentSize based on the actual
	// interblob padding used when the blobs are correctly ordered instead of using worst case padding.
	ss := shares.BlobMinSquareSize(b.currentSize)

	// sort the blobs in order of namespace. We use slice stable here to respect the
	// order of multiple blobs within a namespace as per the priority of the PFB
	sort.SliceStable(b.blobs, func(i, j int) bool {
		return bytes.Compare(fullNamespace(b.blobs[i].blob), fullNamespace(b.blobs[j].blob)) < 0
	})

	// write all the regular transactions into compact shares
	txWriter := shares.NewCompactShareSplitter(namespace.TxNamespace, appconsts.ShareVersionZero)
	for _, tx := range b.txs {
		if err := txWriter.WriteTx(tx); err != nil {
			return nil, fmt.Errorf("writing tx into compact shares: %w", err)
		}
	}

	// begin to iteratively add blobs to the sparse share splitter calculating the actual padding
	nonReservedStart := b.txCounter.Size() + b.pfbCounter.Size()
	cursor := nonReservedStart
	endOfLastBlob := nonReservedStart
	blobWriter := shares.NewSparseShareSplitter()
	for i, element := range b.blobs {
		// NextShareIndex returned where the next blob should start so as to comply with the share commitment rules
		// We fill out the remaining
		cursor, _ = shares.NextShareIndex(cursor, element.numShares, ss, b.subtreeRootThreshold)
		if i == 0 {
			nonReservedStart = cursor
		}

		// defensively check that the actual padding never exceeds the max padding initially allocated for it
		padding := cursor - endOfLastBlob
		if padding > element.maxPadding {
			return nil, fmt.Errorf("blob has %d padding shares, but %d was the max possible", padding, element.maxPadding)
		}

		// record the starting share index of the blob in the PFB that paid for it
		b.pfbs[element.pfbIndex].ShareIndexes[element.blobIndex] = uint32(cursor)
		// If this is not the first blob, we add padding by writing padded shares to the previous blob
		// (which could be of a different namespace)
		if i > 0 {
			if err := blobWriter.WriteNamespacePaddingShares(padding); err != nil {
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
	pfbWriter := shares.NewCompactShareSplitter(namespace.PayForBlobNamespace, appconsts.ShareVersionZero)
	for _, iw := range b.pfbs {
		iwBytes, err := iw.Marshal()
		if err != nil {
			return nil, fmt.Errorf("marshaling pay for blob tx: %w", err)
		}
		if err := pfbWriter.WriteTx(iwBytes); err != nil {
			return nil, fmt.Errorf("writing pay for blob tx into compact shares: %w", err)
		}
	}

	// defensively check that the counter is always greater in share count than the pfbTxWriter.
	if b.pfbCounter.Size() < pfbWriter.Count() {
		return nil, fmt.Errorf("pfbCounter.Size() < pfbTxWriter.Count(): %d < %d", b.pfbCounter.Size(), pfbWriter.Count())
	}

	// Write out the square
	square, err := writeSquare(txWriter, pfbWriter, blobWriter, nonReservedStart, ss)
	if err != nil {
		return nil, fmt.Errorf("writing square: %w", err)
	}

	b.done = true

	return square, nil
}

// FindBlobStartingIndex returns the starting share index of the blob in the square. It takes
// the index of the pfb in the tx set and the index of the blob within the PFB.
func (b *Builder) FindBlobStartingIndex(pfbIndex, blobIndex int) (int, error) {
	if pfbIndex < len(b.txs) {
		return 0, fmt.Errorf("pfbIndex %d does not match a pfb", pfbIndex)
	}
	pfbIndex -= len(b.txs)
	if pfbIndex >= len(b.pfbs) {
		return 0, fmt.Errorf("pfbIndex %d out of range", pfbIndex)
	}
	if blobIndex < 0 {
		return 0, fmt.Errorf("blobIndex %d must not be negative", blobIndex)
	}

	// The share indexes of each blob needs to be computed thus we need to ensure
	// that we have called Export() before we can return the share index of a blob
	if !b.done {
		_, err := b.Export()
		if err != nil {
			return 0, fmt.Errorf("building square: %w", err)
		}
	}

	if blobIndex >= len(b.pfbs[pfbIndex].ShareIndexes) {
		return 0, fmt.Errorf("blobIndex %d out of range", blobIndex)
	}

	return int(b.pfbs[pfbIndex].ShareIndexes[blobIndex]), nil
}

// BlobShareLength returns the amount of shares a blob takes up in the square. It takes
// the index of the pfb in the tx set and the index of the blob within the PFB.
// TODO: we could look in to creating a map to avoid O(n) lookup when we expect large
// numbers of blobs
func (b *Builder) BlobShareLength(pfbIndex, blobIndex int) (int, error) {
	if pfbIndex < len(b.txs) {
		return 0, fmt.Errorf("pfbIndex %d does not match a pfb", pfbIndex)
	}
	pfbIndex -= len(b.txs)
	if pfbIndex >= len(b.pfbs) {
		return 0, fmt.Errorf("pfbIndex %d out of range", pfbIndex)
	}
	if blobIndex < 0 {
		return 0, fmt.Errorf("blobIndex %d must not be negative", blobIndex)
	}

	for _, blob := range b.blobs {
		if blob.pfbIndex == pfbIndex && blob.blobIndex == blobIndex {
			return blob.numShares, nil
		}
	}
	return 0, fmt.Errorf("blob not found")
}

// FindTxShareRange returns the range of shares occupied by the tx at txIndex.
// The indexes are both inclusive.
func (b *Builder) FindTxShareRange(txIndex int) (shares.Range, error) {
	// the square must be built before we can find the share range as we need to compute
	// the wrapped indexes for the PFBs. NOTE: If a tx isn't a PFB, we could theoretically
	// calculate the index without having to build the entire square.
	if !b.done {
		_, err := b.Export()
		if err != nil {
			return shares.Range{}, fmt.Errorf("building square: %w", err)
		}
	}
	if txIndex < 0 {
		return shares.Range{}, fmt.Errorf("txIndex %d must not be negative", txIndex)
	}

	if txIndex >= len(b.txs)+len(b.pfbs) {
		return shares.Range{}, fmt.Errorf("txIndex %d out of range", txIndex)
	}

	txWriter := shares.NewCompactShareCounter()
	pfbWriter := shares.NewCompactShareCounter()
	for i := 0; i < txIndex; i++ {
		if i < len(b.txs) {
			_ = txWriter.Add(len(b.txs[i]))
		} else {
			_ = pfbWriter.Add(b.pfbs[i-len(b.txs)].Size())
		}
	}

	start := txWriter.Size() + pfbWriter.Size() - 1

	// the chosen tx is a regular tx
	if txIndex < len(b.txs) {
		// If the remainder is 0, it means the tx will begin with the next share
		// so we need to increment the start index.
		if txWriter.Remainder() == 0 {
			start++
		}
		_ = txWriter.Add(len(b.txs[txIndex]))
	} else { // the chosen tx is a PFB
		// If the remainder is 0, it means the tx will begin with the next share
		// so we need to increment the start index.
		if pfbWriter.Remainder() == 0 {
			start++
		}
		_ = pfbWriter.Add(b.pfbs[txIndex-len(b.txs)].Size())
	}
	end := txWriter.Size() + pfbWriter.Size()

	return shares.NewRange(start, end), nil
}

func (b *Builder) GetWrappedPFB(txIndex int) (*coretypes.IndexWrapper, error) {
	if txIndex < 0 {
		return nil, fmt.Errorf("txIndex %d must not be negative", txIndex)
	}

	if txIndex < len(b.txs) {
		return nil, fmt.Errorf("txIndex %d does not match a pfb", txIndex)
	}

	if txIndex >= len(b.txs)+len(b.pfbs) {
		return nil, fmt.Errorf("txIndex %d out of range", txIndex)
	}

	if !b.done {
		_, err := b.Export()
		if err != nil {
			return nil, fmt.Errorf("building square: %w", err)
		}
	}

	return b.pfbs[txIndex-len(b.txs)], nil
}

func (b *Builder) NumPFBs() int {
	return len(b.pfbs)
}

func (b *Builder) NumTxs() int {
	return len(b.txs) + len(b.pfbs)
}

func (b *Builder) canFit(shareNum int) bool {
	return b.currentSize+shareNum <= b.maxCapacity
}

func (b *Builder) isEmpty() bool {
	return b.txCounter.Size() == 0 && b.pfbCounter.Size() == 0
}

// TODO: celestia-core should provide this method for `Blob`s
func fullNamespace(blob core.Blob) []byte {
	return append([]byte{byte(blob.NamespaceVersion)}, blob.NamespaceID...)
}

type element struct {
	blob       core.Blob
	pfbIndex   int
	blobIndex  int
	numShares  int
	maxPadding int
}

func newElement(blob core.Blob, pfbIndex, blobIndex, subtreeRootThreshold int) *element {
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
		maxPadding: shares.SubTreeWidth(numShares, subtreeRootThreshold) - 1,
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
