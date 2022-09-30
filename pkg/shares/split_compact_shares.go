package shares

import (
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
	shares       []Share
	pendingShare Share
	namespace    namespace.ID
	version      uint8
}

// NewCompactShareSplitter returns a CompactShareSplitter using the provided
// namespace.
func NewCompactShareSplitter(ns namespace.ID, version uint8) *CompactShareSplitter {
	pendingShare := make([]byte, 0, appconsts.ShareSize)
	infoByte, err := NewInfoByte(version, true)
	if err != nil {
		panic(err)
	}
	placeholderDataLength := make([]byte, appconsts.FirstCompactShareDataLengthBytes)
	pendingShare = append(pendingShare, ns...)
	pendingShare = append(pendingShare, byte(infoByte))
	pendingShare = append(pendingShare, placeholderDataLength...)
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
		css.pendingShare = append(css.pendingShare, reservedBytes...)
	}

	txCursor := len(rawData)
	for txCursor != 0 {
		// find the len left in the pending share
		pendingLeft := appconsts.ShareSize - len(css.pendingShare)

		// if we can simply add the tx to the share without creating a new
		// pending share, do so and return
		if len(rawData) <= pendingLeft {
			css.pendingShare = append(css.pendingShare, rawData...)
			break
		}

		// if we can only add a portion of the transaction to the pending share,
		// then we add it and add the pending share to the finalized shares.
		chunk := rawData[:pendingLeft]
		css.pendingShare = append(css.pendingShare, chunk...)
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

		css.pendingShare = append(css.pendingShare, reservedBytes...)
	}

	// if the share is exactly the correct size, then append to shares
	if len(css.pendingShare) == appconsts.ShareSize {
		css.stackPending()
	}
}

// stackPending will add the pending share to accumlated shares provided that it is long enough
func (css *CompactShareSplitter) stackPending() {
	if len(css.pendingShare) < appconsts.ShareSize {
		return
	}
	css.shares = append(css.shares, css.pendingShare)
	newPendingShare := make([]byte, 0, appconsts.ShareSize)
	newPendingShare = append(newPendingShare, css.namespace...)
	infoByte, err := NewInfoByte(css.version, false)
	if err != nil {
		panic(err)
	}
	newPendingShare = append(newPendingShare, byte(infoByte))
	css.pendingShare = newPendingShare
}

// Export finalizes and returns the underlying compact shares.
func (css *CompactShareSplitter) Export() []Share {
	if css.isEmpty() {
		return []Share{}
	}

	var bytesOfPadding int
	// add the pending share to the current shares before returning
	if !css.isEmptyPendingShare() {
		css.pendingShare, bytesOfPadding = zeroPadIfNecessary(css.pendingShare, appconsts.ShareSize)
		css.shares = append(css.shares, css.pendingShare)
	}

	dataLengthVarint := css.dataLengthVarint(bytesOfPadding)
	css.writeDataLengthVarintToFirstShare(dataLengthVarint)
	return css.shares
}

// dataLengthVarint returns a varint of the data length written to this compact
// share splitter.
func (css *CompactShareSplitter) dataLengthVarint(bytesOfPadding int) []byte {
	if css.isEmpty() {
		return []byte{}
	}

	// declare and initialize the data length
	dataLengthVarint := make([]byte, appconsts.FirstCompactShareDataLengthBytes)
	binary.PutUvarint(dataLengthVarint, css.dataLength(bytesOfPadding))
	zeroPadIfNecessary(dataLengthVarint, appconsts.FirstCompactShareDataLengthBytes)

	return dataLengthVarint
}

func (css *CompactShareSplitter) writeDataLengthVarintToFirstShare(dataLengthVarint []byte) {
	if css.isEmpty() {
		return
	}

	// write the data length varint to the first share
	firstShare := css.shares[0]
	for i := 0; i < appconsts.FirstCompactShareDataLengthBytes; i++ {
		firstShare[appconsts.NamespaceSize+appconsts.ShareInfoBytes+i] = dataLengthVarint[i]
	}

	// replace existing first share with new first share
	css.shares[0] = firstShare
}

// dataLength returns the total length in bytes of all units (transactions,
// intermediate state roots, or evidence) written to this splitter.
// dataLength does not include the # of bytes occupied by the namespace ID or
// the share info byte in each share. dataLength does include the reserved
// byte in each share and the unit length delimiter prefixed to each unit.
func (css *CompactShareSplitter) dataLength(bytesOfPadding int) uint64 {
	if len(css.shares) == 0 {
		return 0
	}
	if len(css.shares) == 1 {
		return uint64(appconsts.FirstCompactShareContentSize) - uint64(bytesOfPadding)
	}

	continuationSharesCount := len(css.shares) - 1
	continuationSharesDataLength := uint64(continuationSharesCount) * appconsts.ContinuationCompactShareContentSize
	return uint64(appconsts.FirstCompactShareContentSize) + continuationSharesDataLength - uint64(bytesOfPadding)
}

// isEmptyPendingShare returns true if the pending share is empty, false otherwise.
func (css *CompactShareSplitter) isEmptyPendingShare() bool {
	if css.isPendingShareTheFirstShare() {
		return len(css.pendingShare) == appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.FirstCompactShareDataLengthBytes
	}
	return len(css.pendingShare) == appconsts.NamespaceSize+appconsts.ShareInfoBytes
}

// isPendingShareTheFirstShare returns true if the pending share is the first
// share of this compact share splitter and false otherwise.
func (css *CompactShareSplitter) isPendingShareTheFirstShare() bool {
	return len(css.shares) == 0
}

// isEmpty returns whether this compact share splitter is empty.
func (css *CompactShareSplitter) isEmpty() bool {
	return len(css.shares) == 0 && css.isEmptyPendingShare()
}

// Count returns the number of shares that would be made if `Export` was invoked
// on this compact share splitter.
func (css *CompactShareSplitter) Count() (shareCount int) {
	if !css.isEmptyPendingShare() {
		// pending share is non-empty, so it will be zero padded and added to shares during export
		return len(css.shares) + 1
	}
	return len(css.shares)
}

// MarshalDelimitedTx prefixes a transaction with the length of the transaction
// encoded as a varint.
func MarshalDelimitedTx(tx coretypes.Tx) ([]byte, error) {
	lenBuf := make([]byte, binary.MaxVarintLen64)
	length := uint64(len(tx))
	n := binary.PutUvarint(lenBuf, length)
	return append(lenBuf[:n], tx...), nil
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}
