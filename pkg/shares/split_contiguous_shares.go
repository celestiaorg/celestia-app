package shares

import (
	"bytes"
	"fmt"

	"github.com/celestiaorg/nmt/namespace"
	"github.com/tendermint/tendermint/libs/protoio"
	"github.com/tendermint/tendermint/pkg/consts"
	coretypes "github.com/tendermint/tendermint/types"
)

// ContiguousShareSplitter will write raw data contiguously across a progressively
// increasing set of shares. It is used to lazily split block data such as transactions
// into shares.
type ContiguousShareSplitter struct {
	shares       []NamespacedShare
	pendingShare NamespacedShare
	namespace    namespace.ID
	version      uint8
}

// NewContiguousShareSplitter returns a ContiguousShareSplitter using the provided
// namespace.
func NewContiguousShareSplitter(ns namespace.ID, version uint8) *ContiguousShareSplitter {
	pendingShare := NamespacedShare{ID: ns, Share: make([]byte, 0, consts.ShareSize)}
	infoByte, err := NewInfoReservedByte(version, true)
	if err != nil {
		panic(err)
	}
	pendingShare.Share = append(pendingShare.Share, ns...)
	pendingShare.Share = append(pendingShare.Share, byte(infoByte))
	return &ContiguousShareSplitter{pendingShare: pendingShare, namespace: ns, version: version}
}

func (csw *ContiguousShareSplitter) WriteTx(tx coretypes.Tx) {
	rawData, err := tx.MarshalDelimited()
	if err != nil {
		panic(fmt.Sprintf("included Tx in mem-pool that can not be encoded %v", tx))
	}
	csw.WriteBytes(rawData)
}

func (csw *ContiguousShareSplitter) WriteEvidence(evd coretypes.Evidence) error {
	pev, err := coretypes.EvidenceToProto(evd)
	if err != nil {
		return err
	}
	rawData, err := protoio.MarshalDelimited(pev)
	if err != nil {
		return err
	}
	csw.WriteBytes(rawData)
	return nil
}

// WriteBytes adds the delimited data to the underlying contiguous shares.
func (csw *ContiguousShareSplitter) WriteBytes(rawData []byte) {
	// if this is the first time writing to a pending share, we must add the
	// reserved bytes
	if len(csw.pendingShare.Share) == consts.NamespaceSize+consts.ShareInfoBytes {
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
		pendingCursor := len(rawData) + consts.NamespaceSize + consts.ShareInfoBytes + consts.ShareReservedBytes
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
func (csw *ContiguousShareSplitter) stackPending() {
	if len(csw.pendingShare.Share) < consts.ShareSize {
		return
	}
	csw.shares = append(csw.shares, csw.pendingShare)
	newPendingShare := make([]byte, 0, consts.ShareSize)
	newPendingShare = append(newPendingShare, csw.namespace...)
	infoByte, err := NewInfoReservedByte(csw.version, false)
	if err != nil {
		panic(err)
	}
	newPendingShare = append(newPendingShare, byte(infoByte))
	csw.pendingShare = NamespacedShare{
		Share: newPendingShare,
		ID:    csw.namespace,
	}
}

// Export finalizes and returns the underlying contiguous shares.
func (csw *ContiguousShareSplitter) Export() NamespacedShares {
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
func (csw *ContiguousShareSplitter) Count() (count, availableBytes int) {
	if len(csw.pendingShare.Share) > consts.NamespaceSize+consts.ShareInfoBytes {
		return len(csw.shares), 0
	}
	//  this doesn't account for the size of the reserved byte
	availableBytes = consts.TxShareSize - (len(csw.pendingShare.Share) - consts.NamespaceSize - consts.ShareInfoBytes)
	return len(csw.shares), availableBytes
}

var tailPaddingInfo, _ = NewInfoReservedByte(consts.ShareVersion, false)

// tail is filler for all tail padded shares
// it is allocated once and used everywhere
var tailPaddingShare = append(append(
	append(make([]byte, 0, consts.ShareSize), consts.TailPaddingNamespaceID...),
	byte(tailPaddingInfo)),
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

func namespacedPaddedShares(ns []byte, count int) []NamespacedShare {
	shares := make([]NamespacedShare, count)
	for i := 0; i < count; i++ {
		shares[i] = NamespacedShare{
			Share: append(append(
				make([]byte, 0, consts.ShareSize), ns...),
				make([]byte, consts.MsgShareSize)...),
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
