package shares

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	coretypes "github.com/tendermint/tendermint/types"
)

// SparseShareSplitter lazily splits messages into shares that will eventually be
// included in a data square. It also has methods to help progressively count
// how many shares the messages written take up.
type SparseShareSplitter struct {
	shares [][]NamespacedShare
	count  int
}

func NewSparseShareSplitter() *SparseShareSplitter {
	return &SparseShareSplitter{}
}

// Write adds the delimited data to the underlying messages shares.
func (sss *SparseShareSplitter) Write(msg coretypes.Message) {
	rawMsg, err := MarshalDelimitedMessage(msg)
	if err != nil {
		panic(fmt.Sprintf("app accepted a Message that can not be encoded %#v", msg))
	}
	newShares := make([]NamespacedShare, 0)
	newShares = AppendToShares(newShares, msg.NamespaceID, rawMsg)
	sss.shares = append(sss.shares, newShares)
	sss.count += len(newShares)
}

// RemoveMessage will remove a message from the underlying message state. If
// there is namespaced padding after the message, then that is also removed.
func (sss *SparseShareSplitter) RemoveMessage(i int) (int, error) {
	j := 1
	initialCount := sss.count
	if len(sss.shares) > i+1 {
		_, msgLen, err := ParseDelimiter(sss.shares[i+1][0].Share[appconsts.NamespaceSize:])
		if err != nil {
			return 0, err
		}
		// 0 means that there is padding after the share that we are about to
		// remove. to remove this padding, we increase j by 1
		// with the message
		if msgLen == 0 {
			j++
			sss.count -= len(sss.shares[j])
		}
	}
	sss.count -= len(sss.shares[i])
	copy(sss.shares[i:], sss.shares[i+j:])
	sss.shares = sss.shares[:len(sss.shares)-j]
	return initialCount - sss.count, nil
}

// WriteNamespacedPaddedShares adds empty shares using the namespace of the last written share.
// This is useful to follow the message layout rules. It assumes that at least
// one share has already been written, if not it panics.
func (sss *SparseShareSplitter) WriteNamespacedPaddedShares(count int) {
	if len(sss.shares) == 0 {
		panic("cannot write empty namespaced shares on an empty SparseShareSplitter")
	}
	if count == 0 {
		return
	}
	lastMessage := sss.shares[len(sss.shares)-1]
	sss.shares = append(sss.shares, namespacedPaddedShares(lastMessage[0].ID, count))
	sss.count += count
}

// Export finalizes and returns the underlying shares.
func (sss *SparseShareSplitter) Export() NamespacedShares {
	shares := make([]NamespacedShare, sss.count)
	cursor := 0
	for _, namespacedShares := range sss.shares {
		for _, share := range namespacedShares {
			shares[cursor] = share
			cursor++
		}
	}
	return shares
}

// Count returns the current number of shares that will be made if exporting.
func (sss *SparseShareSplitter) Count() int {
	return sss.count
}

// AppendToShares appends raw data as shares.
// Used for messages.
func AppendToShares(shares []NamespacedShare, nid namespace.ID, rawData []byte) []NamespacedShare {
	if len(rawData) <= appconsts.SparseShareContentSize {
		infoByte, err := NewInfoReservedByte(appconsts.ShareVersion, true)
		if err != nil {
			panic(err)
		}
		rawShare := append(append(append(
			make([]byte, 0, appconsts.ShareSize),
			nid...),
			byte(infoByte)),
			rawData...,
		)
		paddedShare := zeroPadIfNecessary(rawShare, appconsts.ShareSize)
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
	infoByte, err := NewInfoReservedByte(appconsts.ShareVersion, true)
	if err != nil {
		panic(err)
	}
	firstRawShare := append(append(append(
		make([]byte, 0, appconsts.ShareSize),
		nid...),
		byte(infoByte)),
		rawData[:appconsts.SparseShareContentSize]...,
	)
	shares = append(shares, NamespacedShare{firstRawShare, nid})
	rawData = rawData[appconsts.SparseShareContentSize:]
	for len(rawData) > 0 {
		shareSizeOrLen := min(appconsts.SparseShareContentSize, len(rawData))
		infoByte, err := NewInfoReservedByte(appconsts.ShareVersion, false)
		if err != nil {
			panic(err)
		}
		rawShare := append(append(append(
			make([]byte, 0, appconsts.ShareSize),
			nid...),
			byte(infoByte)),
			rawData[:shareSizeOrLen]...,
		)
		paddedShare := zeroPadIfNecessary(rawShare, appconsts.ShareSize)
		share := NamespacedShare{paddedShare, nid}
		shares = append(shares, share)
		rawData = rawData[shareSizeOrLen:]
	}
	return shares
}
