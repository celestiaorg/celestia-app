package shares

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/tendermint/tendermint/libs/protoio"
	coretypes "github.com/tendermint/tendermint/types"
)

// CompactShareSplitter will write raw data compactly across a progressively
// increasing set of shares. It is used to lazily split block data such as
// transactions, intermediate state roots, and evidence into shares.
type CompactShareSplitter struct {
	shares       []NamespacedShare
	pendingShare NamespacedShare
	namespace    namespace.ID
	version      uint8
}

// NewCompactShareSplitter returns a CompactShareSplitter using the provided
// namespace.
func NewCompactShareSplitter(ns namespace.ID, version uint8) *CompactShareSplitter {
	pendingShare := NamespacedShare{ID: ns, Share: make([]byte, 0, appconsts.ShareSize)}
	infoByte, err := NewInfoReservedByte(version, true)
	if err != nil {
		panic(err)
	}
	placeholderDataLength := make([]byte, appconsts.FirstCompactShareDataLengthBytes)
	pendingShare.Share = append(pendingShare.Share, ns...)
	pendingShare.Share = append(pendingShare.Share, byte(infoByte))
	pendingShare.Share = append(pendingShare.Share, placeholderDataLength...)
	return &CompactShareSplitter{pendingShare: pendingShare, namespace: ns}
}

func (css *CompactShareSplitter) WriteTx(tx coretypes.Tx) {
	rawData, err := MarshalDelimitedTx(tx)
	if err != nil {
		panic(fmt.Sprintf("included Tx in mem-pool that can not be encoded %v", tx))
	}
	css.WriteBytes(rawData)
}

func (css *CompactShareSplitter) WriteEvidence(evd coretypes.Evidence) error {
	pev, err := coretypes.EvidenceToProto(evd)
	if err != nil {
		return err
	}
	rawData, err := protoio.MarshalDelimited(pev)
	if err != nil {
		return err
	}
	css.WriteBytes(rawData)
	return nil
}

// WriteBytes adds the delimited data to the underlying compact shares.
func (css *CompactShareSplitter) WriteBytes(rawData []byte) {
	// if this is the first time writing to a pending share, we must add the
	// reserved bytes
	if css.isEmptyPendingShare() {
		reservedBytes := make([]byte, appconsts.CompactShareReservedBytes)
		css.pendingShare.Share = append(css.pendingShare.Share, reservedBytes...)
	}

	txCursor := len(rawData)
	for txCursor != 0 {
		// find the len left in the pending share
		pendingLeft := appconsts.ShareSize - len(css.pendingShare.Share)

		// if we can simply add the tx to the share without creating a new
		// pending share, do so and return
		if len(rawData) <= pendingLeft {
			css.pendingShare.Share = append(css.pendingShare.Share, rawData...)
			break
		}

		// if we can only add a portion of the transaction to the pending share,
		// then we add it and add the pending share to the finalized shares.
		chunk := rawData[:pendingLeft]
		css.pendingShare.Share = append(css.pendingShare.Share, chunk...)
		css.stackPending()

		// update the cursor
		rawData = rawData[pendingLeft:]
		txCursor = len(rawData)

		// add the share reserved bytes to the new pending share
		pendingCursor := len(rawData) + appconsts.NamespaceSize + appconsts.ShareInfoBytes + appconsts.CompactShareReservedBytes
		reservedBytes := make([]byte, appconsts.CompactShareReservedBytes)
		if pendingCursor >= appconsts.ShareSize {
			// the share reserve byte is zero when some compactly written
			// data takes up the entire share
			for i := range reservedBytes {
				reservedBytes[i] = byte(0)
			}
		} else {
			// TODO this must be changed when share size is increased to 512
			reservedBytes[0] = byte(pendingCursor)
		}

		css.pendingShare.Share = append(css.pendingShare.Share, reservedBytes...)
	}

	// if the share is exactly the correct size, then append to shares
	if len(css.pendingShare.Share) == appconsts.ShareSize {
		css.stackPending()
	}
}

// stackPending will add the pending share to accumlated shares provided that it is long enough
func (css *CompactShareSplitter) stackPending() {
	if len(css.pendingShare.Share) < appconsts.ShareSize {
		return
	}
	css.shares = append(css.shares, css.pendingShare)
	newPendingShare := make([]byte, 0, appconsts.ShareSize)
	newPendingShare = append(newPendingShare, css.namespace...)
	infoByte, err := NewInfoReservedByte(css.version, false)
	if err != nil {
		panic(err)
	}
	newPendingShare = append(newPendingShare, byte(infoByte))
	css.pendingShare = NamespacedShare{
		Share: newPendingShare,
		ID:    css.namespace,
	}
}

// Export finalizes and returns the underlying compact shares.
func (css *CompactShareSplitter) Export() NamespacedShares {
	if len(css.shares) == 0 && css.isEmptyPendingShare() {
		return []NamespacedShare{}
	}

	dataLengthVarint := css.dataLengthVarint()

	// add the pending share to the current shares before returning
	if !css.isEmptyPendingShare() {
		css.pendingShare.Share = zeroPadIfNecessary(css.pendingShare.Share, appconsts.ShareSize)
		css.shares = append(css.shares, css.pendingShare)
		css.pendingShare = NamespacedShare{}
	}

	css.writeDataLengthVarintToFirstShare(dataLengthVarint)
	css.forceLastShareReserveByteToZero()
	return css.shares
}

// forceLastShareReserveByteToZero overwrites the reserve byte of the last share
// with zero. See https://github.com/celestiaorg/celestia-app/issues/779
func (css *CompactShareSplitter) forceLastShareReserveByteToZero() {
	if len(css.shares) == 0 {
		return
	}
	lastShare := css.shares[len(css.shares)-1]
	rawLastShare := lastShare.Data()

	for i := 0; i < appconsts.CompactShareReservedBytes; i++ {
		// here we force the last share reserved byte to be zero to avoid any
		// confusion for light clients parsing these shares, as the rest of the
		// data after transaction is padding. See
		// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/data_structures.md#share
		if len(css.shares) == 1 {
			rawLastShare[appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.FirstCompactShareDataLengthBytes+i] = byte(0)
		} else {
			rawLastShare[appconsts.NamespaceSize+appconsts.ShareInfoBytes+i] = byte(0)
		}
	}

	newLastShare := NamespacedShare{
		Share: rawLastShare,
		ID:    lastShare.NamespaceID(),
	}
	css.shares[len(css.shares)-1] = newLastShare
}

func (css *CompactShareSplitter) dataLengthVarint() []byte {
	if len(css.shares) == 0 && css.isEmptyPendingShare() {
		return []byte{}
	}

	// declare and initialize the data length
	dataLengthVarint := make([]byte, appconsts.FirstCompactShareDataLengthBytes)
	binary.PutUvarint(dataLengthVarint, css.dataLength())
	zeroPadIfNecessary(dataLengthVarint, appconsts.FirstCompactShareDataLengthBytes)

	return dataLengthVarint
}

func (css *CompactShareSplitter) writeDataLengthVarintToFirstShare(dataLengthVarint []byte) {
	if len(css.shares) == 0 && css.isEmptyPendingShare() {
		return
	}

	// write the data length varint to the first share
	firstShare := css.shares[0]
	rawFirstShare := firstShare.Data()
	for i := 0; i < appconsts.FirstCompactShareDataLengthBytes; i++ {
		rawFirstShare[appconsts.NamespaceSize+appconsts.ShareInfoBytes+i] = dataLengthVarint[i]
	}

	// replace existing first share with new first share
	newFirstShare := NamespacedShare{
		Share: rawFirstShare,
		ID:    firstShare.NamespaceID(),
	}
	css.shares[0] = newFirstShare
}

// dataLength returns the total length in bytes of all units (transactions,
// intermediate state roots, or evidence) written to this splitter.
// dataLength does not include the # of bytes occupied by the namespace ID or
// the share info byte in each share. dataLength does include the reserved
// byte in each share and the unit length delimiter prefixed to each unit.
func (css *CompactShareSplitter) dataLength() uint64 {
	// HACKHACK this dataLength calculation is wrong.
	// It doesn't account for the fact that the first compact share has fewer bytes available for data than continuation compact shares.
	length := uint64(len(css.shares)) * appconsts.ContinuationCompactShareContentSize
	length += uint64(len(css.shares)) * appconsts.CompactShareReservedBytes
	if !css.isEmptyPendingShare() {
		length += css.pendingShareDataLength()
	}
	return length
}

// isEmptyPendingShare returns true if the pending share is empty, false otherwise.
func (css *CompactShareSplitter) isEmptyPendingShare() bool {
	return len(css.pendingShare.Share) == appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.FirstCompactShareDataLengthBytes
}

// pendingShareDataLength returns the length of the data in the pending share.
func (css *CompactShareSplitter) pendingShareDataLength() uint64 {
	// HACKHACK this pendingShareDataLength calculation is wrong if the pending share is the first share.
	foo := uint64(len(css.pendingShare.Share) - appconsts.NamespaceSize - appconsts.ShareInfoBytes)
	return foo
}

// Count returns the current number of shares that will be made if exporting.
func (css *CompactShareSplitter) Count() (shareCount int) {
	if len(css.shares) == 0 && css.isEmptyPendingShare() {
		return 0
	}

	if len(css.pendingShare.Share) > appconsts.NamespaceSize+appconsts.ShareInfoBytes {
		// pending share is non-empty, so we must add one to the count
		return len(css.shares) + 1
	}
	return len(css.shares)
}

var tailPaddingInfo, _ = NewInfoReservedByte(appconsts.ShareVersion, false)

// tail is filler for all tail padded shares
// it is allocated once and used everywhere
var tailPaddingShare = append(append(
	append(make([]byte, 0, appconsts.ShareSize), appconsts.TailPaddingNamespaceID...),
	byte(tailPaddingInfo)),
	bytes.Repeat([]byte{0}, appconsts.ShareSize-appconsts.NamespaceSize-appconsts.ShareInfoBytes)...,
)

// TailPaddingShares creates n tail padding shares.
func TailPaddingShares(n int) NamespacedShares {
	shares := make([]NamespacedShare, n)
	for i := 0; i < n; i++ {
		shares[i] = NamespacedShare{
			Share: tailPaddingShare,
			ID:    appconsts.TailPaddingNamespaceID,
		}
	}
	return shares
}

// MarshalDelimitedTx prefixes a transaction with the length of the transaction
// encoded as a varint.
func MarshalDelimitedTx(tx coretypes.Tx) ([]byte, error) {
	lenBuf := make([]byte, binary.MaxVarintLen64)
	length := uint64(len(tx))
	n := binary.PutUvarint(lenBuf, length)
	return append(lenBuf[:n], tx...), nil
}

func namespacedPaddedShares(ns []byte, count int) NamespacedShares {
	infoByte, err := NewInfoReservedByte(appconsts.ShareVersion, true)
	if err != nil {
		panic(err)
	}
	shares := make([]NamespacedShare, count)
	for i := 0; i < count; i++ {
		shares[i] = NamespacedShare{
			Share: append(append(append(
				make([]byte, 0, appconsts.ShareSize), ns...),
				byte(infoByte)),
				make([]byte, appconsts.SparseShareContentSize)...),
			ID: ns,
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
