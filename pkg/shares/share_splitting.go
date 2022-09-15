package shares

import (
	"errors"
	"fmt"

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

func Split(data coretypes.Data) ([][]byte, error) {
	if data.OriginalSquareSize == 0 || !isPowerOf2(data.OriginalSquareSize) {
		return nil, fmt.Errorf("square size is not a power of two: %d", data.OriginalSquareSize)
	}
	wantShareCount := int(data.OriginalSquareSize * data.OriginalSquareSize)
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

	var msgShares [][]byte
	if msgIndexes != nil && int(msgIndexes[0]) != currentShareCount {
		return nil, ErrUnexpectedFirstMessageShareIndex
	}

	msgShares, err = SplitMessages(msgIndexes, data.Messages.MessagesList)
	if err != nil {
		return nil, err
	}
	currentShareCount += len(msgShares)

	tailShares := TailPaddingShares(wantShareCount - currentShareCount).RawShares()

	// todo: optimize using a predefined slice
	shares := append(append(append(
		txShares,
		evdShares...),
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

func SplitTxs(txs coretypes.Txs) [][]byte {
	writer := NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersion)
	for _, tx := range txs {
		writer.WriteTx(tx)
	}
	return writer.Export().RawShares()
}

func SplitEvidence(evd coretypes.EvidenceList) ([][]byte, error) {
	writer := NewCompactShareSplitter(appconsts.EvidenceNamespaceID, appconsts.ShareVersion)
	for _, ev := range evd {
		err := writer.WriteEvidence(ev)
		if err != nil {
			return nil, err
		}
	}
	return writer.Export().RawShares(), nil
}

func SplitMessages(indexes []uint32, msgs []coretypes.Message) ([][]byte, error) {
	if indexes != nil && len(indexes) != len(msgs) {
		return nil, ErrIncorrectNumberOfIndexes
	}
	writer := NewSparseShareSplitter()
	for i, msg := range msgs {
		writer.Write(msg)
		if indexes != nil && len(indexes) > i+1 {
			writer.WriteNamespacedPaddedShares(int(indexes[i+1]) - writer.Count())
		}
	}
	return writer.Export().RawShares(), nil
}
