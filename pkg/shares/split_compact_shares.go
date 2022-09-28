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
	pendingShare.Share = append(pendingShare.Share, ns...)
	pendingShare.Share = append(pendingShare.Share, byte(infoByte))
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
	if len(css.pendingShare.Share) == appconsts.NamespaceSize+appconsts.ShareInfoBytes {
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
	// add the pending share to the current shares before returning
	if len(css.pendingShare.Share) > appconsts.NamespaceSize+appconsts.ShareInfoBytes {
		css.pendingShare.Share, _ = zeroPadIfNecessary(css.pendingShare.Share, appconsts.ShareSize)
		css.shares = append(css.shares, css.pendingShare)
	}
	// force the last share to have a reserve byte of zero
	if len(css.shares) == 0 {
		return css.shares
	}
	lastShare := css.shares[len(css.shares)-1]
	rawLastShare := lastShare.Data()

	for i := 0; i < appconsts.CompactShareReservedBytes; i++ {
		// here we force the last share reserved byte to be zero to avoid any
		// confusion for light clients parsing these shares, as the rest of the
		// data after transaction is padding. See
		// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/data_structures.md#share
		rawLastShare[appconsts.NamespaceSize+appconsts.ShareInfoBytes+i] = byte(0)
	}

	newLastShare := NamespacedShare{
		Share: rawLastShare,
		ID:    lastShare.NamespaceID(),
	}
	css.shares[len(css.shares)-1] = newLastShare
	return css.shares
}

// Count returns the current number of shares that will be made if exporting.
func (css *CompactShareSplitter) Count() (shareCount int) {
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
