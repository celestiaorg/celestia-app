package square

import (
	"bytes"
	"fmt"
	"math"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/tendermint/tendermint/pkg/consts"
	coretypes "github.com/tendermint/tendermint/proto/tendermint/types"
	core "github.com/tendermint/tendermint/types"
)

func Construct(txs [][]byte, maxSquareSize int) (Square, [][]byte) {
	constructor := NewConstructor(maxSquareSize)
	normalTxs := make([][]byte, 0, len(txs))
	blobTxs := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		blobTx, isBlobTx := core.UnmarshalBlobTx(tx)
		if isBlobTx {
			if constructor.InsertBlobTx(blobTx) {
				blobTxs = append(blobTxs, tx)
			}
		} else {
			if constructor.InsertTx(tx) {
				normalTxs = append(normalTxs, tx)
			}
		}
	}
	square, _ := constructor.Complete()
	resultTxs := append(normalTxs, blobTxs...)
	return square, resultTxs
}

func Reconstruct(txs [][]byte, maxSquareSize int) (Square, error) {
	constructor := NewConstructor(maxSquareSize)
	seenFirstBlobTx := false
	for idx, tx := range txs {
		blobTx, isBlobTx := core.UnmarshalBlobTx(tx)
		if isBlobTx {
			seenFirstBlobTx = true
			if !constructor.InsertBlobTx(blobTx) {
				return nil, fmt.Errorf("blob tx at index %d can not be inserted", idx)
			}
		} else {
			if seenFirstBlobTx {
				return nil, fmt.Errorf("normal tx at index %d can not be inserted after blob tx", idx)
			}
			if !constructor.InsertTx(tx) {
				return nil, fmt.Errorf("tx at index %d can not be inserted", idx)
			}
		}
	}
	square, _ := constructor.Complete()
	return square, nil
}

type Square []shares.Share

func (s Square) Size() uint64 {
	return uint64(math.Sqrt(float64(len(s))))
}

type Constructor struct {
	maxPossibleShares  int // upper bound on square size
	currentMaxCapacity int // in shares
	txs                [][]byte
	txCounter          *shares.CompactShareCounter
	pfbs               []*coretypes.IndexWrapper
	pfbCounter         *shares.CompactShareCounter
	blobElements       []*element
}

func NewConstructor(maxSquareSize int) *Constructor {
	return &Constructor{
		maxPossibleShares: maxSquareSize * maxSquareSize,
		blobElements:      make([]*element, 0),
		pfbs:              make([]*coretypes.IndexWrapper, 0),
		txs:               make([][]byte, 0),
		txCounter:         shares.NewCounter(),
		pfbCounter:        shares.NewCounter(),
	}
}

func (c *Constructor) InsertTx(tx []byte) bool {
	lenChange := c.txCounter.Add(len(tx))
	if c.canFit(lenChange) {
		c.txs = append(c.txs, tx)
		c.currentMaxCapacity += lenChange
		return true
	}
	c.txCounter.RevertLast()
	return false
}

func (c *Constructor) InsertBlobTx(blobTx coretypes.BlobTx) bool {
	iw := &coretypes.IndexWrapper{
		Tx:           blobTx.Tx,
		TypeId:       consts.ProtoIndexWrapperTypeID,
		ShareIndexes: make([]uint32, len(blobTx.Blobs)),
	}
	for idx := range blobTx.Blobs {
		iw.ShareIndexes[idx] = uint32(c.maxPossibleShares)
	}
	size := iw.Size()
	pfbShareDiff := c.pfbCounter.Add(size)

	blobElements := make([]*element, len(blobTx.Blobs))
	maxBlobShareCount := 0
	for idx, blobProto := range blobTx.Blobs {
		blob := core.BlobFromProto(blobProto)
		blobElements[idx] = newElement(blob, len(c.pfbs), idx)
		maxBlobShareCount += blobElements[idx].maxShareOffset()
	}

	if c.canFit(pfbShareDiff + maxBlobShareCount) {
		c.blobElements = append(c.blobElements, blobElements...)
		c.pfbs = append(c.pfbs, iw)
		c.currentMaxCapacity += (pfbShareDiff + maxBlobShareCount)
		return true
	}
	c.pfbCounter.RevertLast()
	return false
}

func (c *Constructor) Complete() (square []shares.Share, pfbs [][]byte) {
	if c.empty() {
		return shares.TailPaddingShares(1), [][]byte{}
	}
	ss := shares.MinSquareSize(c.currentMaxCapacity)
	square = make([]shares.Share, ss*ss)
	sort.SliceStable(c.blobElements, func(i, j int) bool {
		return bytes.Compare(c.blobElements[i].blob.NamespaceID, c.blobElements[j].blob.NamespaceID) < 0
	})

	txShares := shares.NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersionZero)
	for _, tx := range c.txs {
		txShares.WriteTx(tx)
	}

	nonReservedStart := c.txCounter.Size() + c.pfbCounter.Size()
	cursor := nonReservedStart
	endOfLastBlob := nonReservedStart
	blobWriter := shares.NewSparseShareSplitter()
	for i, element := range c.blobElements {
		cursor, _ = shares.NextMultipleOfBlobMinSquareSize(cursor, element.shareSize, ss)
		if i == 0 {
			nonReservedStart = cursor
		}
		if cursor-endOfLastBlob > element.maxPadding {
			panic(fmt.Sprintf("blob has %d padding shares, but %d was the max possible", cursor-endOfLastBlob, element.maxPadding))
		}
		c.pfbs[element.pfbIndex].ShareIndexes[element.blobIndex] = uint32(cursor)
		if i > 0 {
			blobWriter.WriteNamespacedPaddedShares(cursor - endOfLastBlob)
		}
		blobWriter.Write(element.blob)
		cursor += element.shareSize
		endOfLastBlob = cursor
	}

	pfbTxWriter := shares.NewCompactShareSplitter(appconsts.PayForBlobNamespaceID, appconsts.ShareVersionZero)
	pfbs = make([][]byte, len(c.pfbs))
	for i, iw := range c.pfbs {
		iwBytes, err := iw.Marshal()
		if err != nil {
			panic(err)
		}
		pfbs[i] = iwBytes
		pfbTxWriter.WriteTx(iwBytes)
	}
	if c.pfbCounter.Size() < pfbTxWriter.Count() {
		panic(fmt.Sprintf("pfbCounter.Size() < pfbTxWriter.Count(): %d < %d", c.pfbCounter.Size(), pfbTxWriter.Count()))
	}
	pfbStartIndex := txShares.Count()
	paddingStartIndex := pfbStartIndex + pfbTxWriter.Count()
	padding := shares.NamespacedPaddedShares(appconsts.ReservedNamespacePadding, nonReservedStart-paddingStartIndex)
	tailShares := shares.TailPaddingShares(len(square) - endOfLastBlob)

	copy(square[:], txShares.Export())
	copy(square[pfbStartIndex:], pfbTxWriter.Export())
	if len(c.blobElements) > 0 {
		copy(square[paddingStartIndex:], padding)
		copy(square[nonReservedStart:], blobWriter.Export())
	}
	copy(square[endOfLastBlob:], tailShares)

	return square, pfbs
}

func (c *Constructor) canFit(shareNum int) bool {
	return c.currentMaxCapacity+shareNum <= c.maxPossibleShares
}

func (c *Constructor) empty() bool {
	return c.txCounter.Size() == 0 && c.pfbCounter.Size() == 0
}

type element struct {
	blob       core.Blob
	pfbIndex   int
	blobIndex  int
	shareSize  int
	maxPadding int
}

func newElement(blob core.Blob, pfbIndex, blobIndex int) *element {
	shareSize := shares.SparseSharesNeeded(uint32(len(blob.Data)))
	return &element{
		blob:       blob,
		pfbIndex:   pfbIndex,
		blobIndex:  blobIndex,
		shareSize:  shareSize,
		maxPadding: shares.MinSquareSize(shareSize) - 1,
	}
}

func (e element) maxShareOffset() int {
	return e.shareSize + e.maxPadding
}
