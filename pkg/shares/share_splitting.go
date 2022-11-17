package shares

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	coretypes "github.com/tendermint/tendermint/types"
)

var (
	ErrIncorrectNumberOfIndexes = errors.New(
		"number of malleated transactions is not identical to the number of wrapped transactions",
	)
	ErrUnexpectedFirstMessageShareIndex = errors.New(
		"the first message started at an unexpected index",
	)
)

// Split converts block data into encoded shares, optionally using share indexes
// that are encoded as wrapped transactions. Most use cases out of this package
// should use these share indexes and therefore set useShareIndexes to true.
func Split(data coretypes.Data, useShareIndexes bool) ([]Share, error) {
	if data.SquareSize == 0 || !isPowerOf2(data.SquareSize) {
		return nil, fmt.Errorf("square size is not a power of two: %d", data.SquareSize)
	}
	wantShareCount := int(data.SquareSize * data.SquareSize)
	currentShareCount := 0

	txShares := SplitTxs(data.Txs)
	currentShareCount += len(txShares)

	evdShares, err := SplitEvidence(data.Evidence.Evidence)
	if err != nil {
		return nil, err
	}
	currentShareCount += len(evdShares)

	// msgIndexes will be nil if we are working with a list of txs that do not
	// have a msg index. this preserves backwards compatibility with old blocks
	// that do not follow the non-interactive defaults
	msgIndexes := ExtractShareIndexes(data.Txs)
	sort.Slice(msgIndexes, func(i, j int) bool { return msgIndexes[i] < msgIndexes[j] })

	var padding []Share
	if len(data.Blobs) > 0 {
		msgShareStart, _ := NextAlignedPowerOfTwo(
			currentShareCount,
			MsgSharesUsed(len(data.Blobs[0].Data)),
			int(data.SquareSize),
		)
		ns := appconsts.TxNamespaceID
		if len(evdShares) > 0 {
			ns = appconsts.EvidenceNamespaceID
		}
		padding = namespacedPaddedShares(ns, msgShareStart-currentShareCount)
	}
	currentShareCount += len(padding)

	var msgShares []Share
	if msgIndexes != nil && int(msgIndexes[0]) < currentShareCount {
		return nil, ErrUnexpectedFirstMessageShareIndex
	}

	msgShares, err = SplitMessages(currentShareCount, msgIndexes, data.Blobs, useShareIndexes)
	if err != nil {
		return nil, err
	}
	currentShareCount += len(msgShares)
	tailShares := TailPaddingShares(wantShareCount - currentShareCount)

	// todo: optimize using a predefined slice
	shares := append(append(append(append(
		txShares,
		evdShares...),
		padding...),
		msgShares...),
		tailShares...)

	return shares, nil
}

// ExtractShareIndexes iterates over the transactions and extracts the share
// indexes from wrapped transactions. It returns nil if the transactions are
// from an old block that did not have share indexes in the wrapped txs.
func ExtractShareIndexes(txs coretypes.Txs) []uint32 {
	var msgIndexes []uint32
	for _, rawTx := range txs {
		if malleatedTx, isMalleated := coretypes.UnwrapMalleatedTx(rawTx); isMalleated {
			// Since share index == 0 is invalid, it indicates that we are
			// attempting to extract share indexes from txs that do not have any
			// due to them being old. here we return nil to indicate that we are
			// attempting to extract indexes from a block that doesn't support
			// it. It's check for 0 because if there is a message in the block,
			// then there must also be a tx, which will take up at least one
			// share.
			if malleatedTx.ShareIndex == 0 {
				return nil
			}
			msgIndexes = append(msgIndexes, malleatedTx.ShareIndex)
		}
	}

	return msgIndexes
}

func SplitTxs(txs coretypes.Txs) []Share {
	writer := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersion)
	for _, tx := range txs {
		writer.WriteTx(tx)
	}
	return writer.Export()
}

func SplitEvidence(evd coretypes.EvidenceList) ([]Share, error) {
	writer := NewCompactShareSplitter(appconsts.EvidenceNamespaceID, appconsts.ShareVersion)
	for _, ev := range evd {
		err := writer.WriteEvidence(ev)
		if err != nil {
			return nil, err
		}
	}
	return writer.Export(), nil
}

func SplitMessages(cursor int, indexes []uint32, blobs []coretypes.Blob, useShareIndexes bool) ([]Share, error) {
	if useShareIndexes && len(indexes) != len(blobs) {
		return nil, ErrIncorrectNumberOfIndexes
	}
	writer := NewSparseShareSplitter()
	for i, msg := range blobs {
		writer.Write(msg)
		if useShareIndexes && len(indexes) > i+1 {
			paddedShareCount := int(indexes[i+1]) - (writer.Count() + cursor)
			writer.WriteNamespacedPaddedShares(paddedShareCount)
		}
	}
	return writer.Export(), nil
}

var tailPaddingInfo, _ = NewInfoByte(appconsts.ShareVersion, false)

// tail is filler for all tail padded shares
// it is allocated once and used everywhere
var tailPaddingShare = append(append(
	append(make([]byte, 0, appconsts.ShareSize), appconsts.TailPaddingNamespaceID...),
	byte(tailPaddingInfo)),
	bytes.Repeat([]byte{0}, appconsts.ShareSize-appconsts.NamespaceSize-appconsts.ShareInfoBytes)...,
)

// TailPaddingShares creates n tail padding shares.
func TailPaddingShares(n int) []Share {
	shares := make([]Share, n)
	for i := 0; i < n; i++ {
		shares[i] = tailPaddingShare
	}
	return shares
}
