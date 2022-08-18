package types

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/celestiaorg/nmt/namespace"
	"github.com/tendermint/tendermint/libs/protoio"
	"github.com/tendermint/tendermint/pkg/consts"
	coretypes "github.com/tendermint/tendermint/types"
)

// MessageShareWriter lazily merges messages into shares that will eventually be
// included in a data square. It also has methods to help progressively count
// how many shares the messages written take up.
type MessageShareWriter struct {
	shares [][]NamespacedShare
	count  int
}

func NewMessageShareWriter() *MessageShareWriter {
	return &MessageShareWriter{}
}

// Write adds the delimited data to the underlying contiguous shares.
func (msw *MessageShareWriter) Write(msg coretypes.Message) {
	rawMsg, err := msg.MarshalDelimited()
	if err != nil {
		panic(fmt.Sprintf("app accepted a Message that can not be encoded %#v", msg))
	}
	newShares := make([]NamespacedShare, 0)
	newShares = AppendToShares(newShares, msg.NamespaceID, rawMsg)
	msw.shares = append(msw.shares, newShares)
	msw.count += len(newShares)
}

// Export finalizes and returns the underlying contiguous shares.
func (msw *MessageShareWriter) Export() NamespacedShares {
	msw.sortMsgs()
	shares := make([]NamespacedShare, msw.count)
	cursor := 0
	for _, messageShares := range msw.shares {
		for _, share := range messageShares {
			shares[cursor] = share
			cursor++
		}
	}
	return shares
}

func (msw *MessageShareWriter) sortMsgs() {
	sort.Slice(msw.shares, func(i, j int) bool {
		return bytes.Compare(msw.shares[i][0].ID, msw.shares[j][0].ID) < 0
	})
}

// Count returns the current number of shares that will be made if exporting.
func (msw *MessageShareWriter) Count() int {
	return msw.count
}

// appendToShares appends raw data as shares.
// Used for messages.
func AppendToShares(shares []NamespacedShare, nid namespace.ID, rawData []byte) []NamespacedShare {
	if len(rawData) <= consts.MsgShareSize {
		rawShare := append(append(
			make([]byte, 0, len(nid)+len(rawData)),
			nid...),
			rawData...,
		)
		paddedShare := zeroPadIfNecessary(rawShare, consts.ShareSize)
		share := NamespacedShare{paddedShare, nid}
		shares = append(shares, share)
	} else { // len(rawData) > MsgShareSize
		shares = append(shares, splitMessage(rawData, nid)...)
	}
	return shares
}

// splitMessage breaks the data in a message into the minimum number of
// namespaced shares
func splitMessage(rawData []byte, nid namespace.ID) NamespacedShares {
	shares := make([]NamespacedShare, 0)
	firstRawShare := append(append(
		make([]byte, 0, consts.ShareSize),
		nid...),
		rawData[:consts.MsgShareSize]...,
	)
	shares = append(shares, NamespacedShare{firstRawShare, nid})
	rawData = rawData[consts.MsgShareSize:]
	for len(rawData) > 0 {
		shareSizeOrLen := min(consts.MsgShareSize, len(rawData))
		rawShare := append(append(
			make([]byte, 0, consts.ShareSize),
			nid...),
			rawData[:shareSizeOrLen]...,
		)
		paddedShare := zeroPadIfNecessary(rawShare, consts.ShareSize)
		share := NamespacedShare{paddedShare, nid}
		shares = append(shares, share)
		rawData = rawData[shareSizeOrLen:]
	}
	return shares
}

// ContiguousShareWriter will write raw data contiguously across a progressively
// increasing set of shares. It is used to lazily split block data such as transactions
// into shares.
type ContiguousShareWriter struct {
	shares       []NamespacedShare
	pendingShare NamespacedShare
	namespace    namespace.ID
}

// NewContiguousShareWriter returns a ContigousShareWriter using the provided
// namespace.
func NewContiguousShareWriter(ns namespace.ID) *ContiguousShareWriter {
	pendingShare := NamespacedShare{ID: ns, Share: make([]byte, 0, consts.ShareSize)}
	pendingShare.Share = append(pendingShare.Share, ns...)
	return &ContiguousShareWriter{pendingShare: pendingShare, namespace: ns}
}

// Write adds the delimited data to the underlying contiguous shares.
func (csw *ContiguousShareWriter) Write(rawData []byte) {
	// if this is the first time writing to a pending share, we must add the
	// reserved bytes
	if len(csw.pendingShare.Share) == consts.NamespaceSize {
		csw.pendingShare.Share = append(csw.pendingShare.Share, 0)
	}

	txCursor := len(rawData)
	for txCursor != 0 {
		// find the len left in the pending share
		pendingLeft := consts.ShareSize - len(csw.pendingShare.Share)

		// if we can simply add the tx to the share without creating a new
		// pending share, do so and return
		if len(rawData) <= pendingLeft {
			csw.pendingShare.Share = append(csw.pendingShare.Share, rawData...)
			break
		}

		// if we can only add a portion of the transaction to the pending share,
		// then we add it and add the pending share to the finalized shares.
		chunk := rawData[:pendingLeft]
		csw.pendingShare.Share = append(csw.pendingShare.Share, chunk...)
		csw.stackPending()

		// update the cursor
		rawData = rawData[pendingLeft:]
		txCursor = len(rawData)

		// add the share reserved bytes to the new pending share
		pendingCursor := len(rawData) + consts.NamespaceSize + consts.ShareReservedBytes
		var reservedByte byte
		if pendingCursor >= consts.ShareSize {
			// the share reserve byte is zero when some contiguously written
			// data takes up the entire share
			reservedByte = byte(0)
		} else {
			reservedByte = byte(pendingCursor)
		}

		csw.pendingShare.Share = append(csw.pendingShare.Share, reservedByte)
	}

	// if the share is exactly the correct size, then append to shares
	if len(csw.pendingShare.Share) == consts.ShareSize {
		csw.stackPending()
	}
}

// stackPending will add the pending share to accumlated shares provided that it is long enough
func (csw *ContiguousShareWriter) stackPending() {
	if len(csw.pendingShare.Share) < consts.ShareSize {
		return
	}
	csw.shares = append(csw.shares, csw.pendingShare)
	newPendingShare := make([]byte, 0, consts.ShareSize)
	newPendingShare = append(newPendingShare, csw.namespace...)
	csw.pendingShare = NamespacedShare{
		Share: newPendingShare,
		ID:    csw.namespace,
	}
}

// Export finalizes and returns the underlying contiguous shares.
func (csw *ContiguousShareWriter) Export() NamespacedShares {
	// add the pending share to the current shares before returning
	if len(csw.pendingShare.Share) > consts.NamespaceSize {
		csw.pendingShare.Share = zeroPadIfNecessary(csw.pendingShare.Share, consts.ShareSize)
		csw.shares = append(csw.shares, csw.pendingShare)
	}
	// force the last share to have a reserve byte of zero
	if len(csw.shares) == 0 {
		return csw.shares
	}
	lastShare := csw.shares[len(csw.shares)-1]
	rawLastShare := lastShare.Data()

	for i := 0; i < consts.ShareReservedBytes; i++ {
		// here we force the last share reserved byte to be zero to avoid any
		// confusion for light clients parsing these shares, as the rest of the
		// data after transaction is padding. See
		// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/data_structures.md#share
		rawLastShare[consts.NamespaceSize+i] = byte(0)
	}

	newLastShare := NamespacedShare{
		Share: rawLastShare,
		ID:    lastShare.NamespaceID(),
	}
	csw.shares[len(csw.shares)-1] = newLastShare
	return csw.shares
}

// Count returns the current number of shares that will be made if exporting.
func (csw *ContiguousShareWriter) Count() (count, availableBytes int) {
	availableBytes = consts.TxShareSize - (len(csw.pendingShare.Share) - consts.NamespaceSize)
	return len(csw.shares), availableBytes
}

// tail is filler for all tail padded shares
// it is allocated once and used everywhere
var tailPaddingShare = append(
	append(make([]byte, 0, consts.ShareSize), consts.TailPaddingNamespaceID...),
	bytes.Repeat([]byte{0}, consts.ShareSize-consts.NamespaceSize)...,
)

func TailPaddingShares(n int) NamespacedShares {
	shares := make([]NamespacedShare, n)
	for i := 0; i < n; i++ {
		shares[i] = NamespacedShare{
			Share: tailPaddingShare,
			ID:    consts.TailPaddingNamespaceID,
		}
	}
	return shares
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

func zeroPadIfNecessary(share []byte, width int) []byte {
	oldLen := len(share)
	if oldLen < width {
		missingBytes := width - oldLen
		padByte := []byte{0}
		padding := bytes.Repeat(padByte, missingBytes)
		share = append(share, padding...)
		return share
	}
	return share
}

func SplitTxsIntoShares(txs coretypes.Txs) NamespacedShares {
	rawDatas := make([][]byte, len(txs))
	for i, tx := range txs {
		rawData, err := tx.MarshalDelimited()
		if err != nil {
			panic(fmt.Sprintf("included Tx in mem-pool that can not be encoded %v", tx))
		}
		rawDatas[i] = rawData
	}

	w := NewContiguousShareWriter(consts.TxNamespaceID)
	for _, tx := range rawDatas {
		w.Write(tx)
	}

	return w.Export()
}

func SplitEvidenceIntoShares(data *coretypes.EvidenceData) NamespacedShares {
	rawDatas := make([][]byte, 0, len(data.Evidence))
	for _, ev := range data.Evidence {
		pev, err := coretypes.EvidenceToProto(ev)
		if err != nil {
			panic("failure to convert evidence to equivalent proto type")
		}
		rawData, err := protoio.MarshalDelimited(pev)
		if err != nil {
			panic(err)
		}
		rawDatas = append(rawDatas, rawData)
	}
	w := NewContiguousShareWriter(consts.EvidenceNamespaceID)
	for _, evd := range rawDatas {
		w.Write(evd)
	}
	return w.Export()
}

func SplitMessagesIntoShares(msgs coretypes.Messages) NamespacedShares {
	shares := make([]NamespacedShare, 0)
	msgs.SortMessages()
	for _, m := range msgs.MessagesList {
		rawData, err := m.MarshalDelimited()
		if err != nil {
			panic(fmt.Sprintf("app accepted a Message that can not be encoded %#v", m))
		}
		shares = AppendToShares(shares, m.NamespaceID, rawData)
	}
	return shares
}

// SortMessages sorts messages by ascending namespace id
func SortMessages(msgs *coretypes.Messages) {
	sort.SliceStable(msgs.MessagesList, func(i, j int) bool {
		return bytes.Compare(msgs.MessagesList[i].NamespaceID, msgs.MessagesList[j].NamespaceID) < 0
	})
}

// ComputeShares splits block data into shares of an original data square and
// returns them along with an amount of non-redundant shares. If a square size
// of 0 is passed, then it is determined based on how many shares are needed to
// fill the square for the underlying block data. The square size is stored in
// the local instance of the struct.
func ComputeShares(data *coretypes.Data, squareSize uint64) (NamespacedShares, int, error) {
	if squareSize != 0 {
		if !powerOf2(squareSize) {
			return nil, 0, errors.New("square size is not a power of two")
		}
	}

	// reserved shares:
	txShares := SplitTxsIntoShares(data.Txs)
	evidenceShares := SplitEvidenceIntoShares(&data.Evidence)

	// application data shares from messages:
	msgShares := SplitMessagesIntoShares(data.Messages)
	curLen := len(txShares) + len(evidenceShares) + len(msgShares)

	if curLen > consts.MaxShareCount {
		panic(fmt.Sprintf("Block data exceeds the max square size. Number of shares required: %d\n", curLen))
	}

	// find the number of shares needed to create a square that has a power of
	// two width
	wantLen := int(squareSize * squareSize)
	if squareSize == 0 {
		wantLen = paddedLen(curLen)
	}

	if wantLen < curLen {
		return nil, 0, errors.New("square size too small to fit block data")
	}

	// ensure that the min square size is used
	if wantLen < consts.MinSharecount {
		wantLen = consts.MinSharecount
	}

	tailShares := TailPaddingShares(wantLen - curLen)

	shares := append(append(append(
		txShares,
		evidenceShares...),
		msgShares...),
		tailShares...)

	if squareSize == 0 {
		squareSize = uint64(math.Sqrt(float64(wantLen)))
	}

	data.OriginalSquareSize = squareSize

	return shares, curLen, nil
}

// paddedLen calculates the number of shares needed to make a power of 2 square
// given the current number of shares
func paddedLen(length int) int {
	width := uint32(math.Ceil(math.Sqrt(float64(length))))
	width = nextHighestPowerOf2(width)
	return int(width * width)
}

// nextPowerOf2 returns the next highest power of 2 unless the input is a power
// of two, in which case it returns the input
func nextHighestPowerOf2(v uint32) uint32 {
	if v == 0 {
		return 0
	}

	// find the next highest power using bit mashing
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	v++

	// return the next highest power
	return v
}

// powerOf2 checks if number is power of 2
func powerOf2(v uint64) bool {
	if v&(v-1) == 0 && v != 0 {
		return true
	}
	return false
}
