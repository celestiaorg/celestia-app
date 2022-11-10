package app

import (
	"bytes"
	"math"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/cosmos/cosmos-sdk/client"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// prune removes txs until the set of txs will fit in the square of size
// squareSize. It assumes that the currentShareCount is accurate. This function
// is far from optimal because accurately knowing how many shares any given
// set of transactions and its message takes up in a data square that is following the
// non-interactive default rules requires recalculating the entire square.
// TODO: include the padding used by each msg when counting removed shares
func prune(txConf client.TxConfig, txs []*parsedTx, currentShareCount, squareSize int) parsedTxs {
	maxShares := squareSize * squareSize
	if maxShares >= currentShareCount {
		return txs
	}
	goal := currentShareCount - maxShares

	removedContiguousShares := 0
	contigBytesCursor := 0
	removedMessageShares := 0
	removedTxs := 0

	// adjustContigCursor checks if enough contiguous bytes have been removed
	// inorder to tally total contiguous shares removed
	adjustContigCursor := func(l int) {
		contigBytesCursor += l + shares.DelimLen(uint64(l))
		if contigBytesCursor >= appconsts.ContinuationCompactShareContentSize {
			removedContiguousShares += (contigBytesCursor / appconsts.ContinuationCompactShareContentSize)
			contigBytesCursor = contigBytesCursor % appconsts.ContinuationCompactShareContentSize
		}
	}

	for i := len(txs) - 1; (removedContiguousShares + removedMessageShares) < goal; i-- {
		// this normally doesn't happen, but since we don't calculate the number
		// of padded shares also being removed, its possible to reach this value
		// should there be many small messages, and we don't want to panic.
		if i < 0 {
			break
		}
		removedTxs++
		if txs[i].msg == nil {
			adjustContigCursor(len(txs[i].rawTx))
			continue
		}

		removedMessageShares += shares.MsgSharesUsed(len(txs[i].msg.GetMessage()))
		// we ignore the error here, as if there is an error malleating the tx,
		// then we need to remove it anyway and it will not end up contributing
		// bytes to the square anyway.
		_ = txs[i].malleate(txConf, uint64(squareSize))
		adjustContigCursor(len(txs[i].malleatedTx) + appconsts.MalleatedTxBytes)
	}

	return txs[:len(txs)-(removedTxs)]
}

// calculateCompactShareCount calculates the exact number of compact shares used.
func calculateCompactShareCount(txs []*parsedTx, evd core.EvidenceList, squareSize int) int {
	txSplitter := shares.NewCompactShareSplitter(appconsts.TxNamespaceID, appconsts.ShareVersion)
	evdSplitter := shares.NewCompactShareSplitter(appconsts.EvidenceNamespaceID, appconsts.ShareVersion)
	var err error
	msgSharesCursor := len(txs)
	for _, tx := range txs {
		rawTx := tx.rawTx
		if tx.malleatedTx != nil {
			rawTx, err = coretypes.WrapMalleatedTx(tx.originalHash(), uint32(msgSharesCursor), tx.malleatedTx)
			if err != nil {
				panic(err)
			}
			used, _ := shares.MsgSharesUsedNonInteractiveDefaults(msgSharesCursor, squareSize, tx.msg.Size())
			msgSharesCursor += used
		}
		txSplitter.WriteTx(rawTx)
	}
	for _, e := range evd.Evidence {
		evidence, err := coretypes.EvidenceFromProto(&e)
		if err != nil {
			panic(err)
		}
		err = evdSplitter.WriteEvidence(evidence)
		if err != nil {
			panic(err)
		}
	}
	return txSplitter.Count() + evdSplitter.Count()
}

// estimateSquareSize uses the provided block data to estimate the square size
// assuming that all malleated txs follow the non interactive default rules.
// Returns the estimated square size and the number of shares used.
func estimateSquareSize(txs []*parsedTx, evd core.EvidenceList) (uint64, int) {
	// get the raw count of shares taken by each type of block data
	txShares, evdShares, msgLens := rawShareCount(txs, evd)
	msgShares := 0
	for _, msgLen := range msgLens {
		msgShares += msgLen
	}

	// calculate the smallest possible square size that could contain all the
	// messages
	squareSize := shares.RoundUpPowerOfTwo(int(math.Ceil(math.Sqrt(float64(txShares + evdShares + msgShares)))))

	// the starting square size should at least be the minimum
	if squareSize < appconsts.MinSquareSize {
		squareSize = appconsts.MinSquareSize
	}

	var fits bool
	for {
		// assume that all the msgs in the square use the non-interactive
		// default rules and see if we can fit them in the smallest starting
		// square size. We start the cursor (share index) at the beginning of
		// the message shares (txShares+evdShares), because shares that do not
		// follow the non-interactive defaults are simple to estimate.
		fits, msgShares = shares.FitsInSquare(txShares+evdShares, squareSize, msgLens...)
		switch {
		// stop estimating if we know we can reach the max square size
		case squareSize >= appconsts.MaxSquareSize:
			return appconsts.MaxSquareSize, txShares + evdShares + msgShares
		// return if we've found a square size that fits all of the txs
		case fits:
			return uint64(squareSize), txShares + evdShares + msgShares
		// try the next largest square size if we can't fit all the txs
		case !fits:
			// double the square size
			squareSize = shares.RoundUpPowerOfTwo(squareSize + 1)
		}
	}
}

// rawShareCount calculates the number of shares taken by all of the included
// txs, evidence, and each msg. msgLens is a slice of the number of shares used
// by each message without accounting for the non-interactive message layout
// rules.
func rawShareCount(txs []*parsedTx, evd core.EvidenceList) (txShares, evdShares int, msgLens []int) {
	// msgSummary is used to keep track of the size and the namespace so that we
	// can sort the messages by namespace before returning.
	type msgSummary struct {
		// size is the number of shares used by this message
		size      int
		namespace []byte
	}

	var msgSummaries []msgSummary //nolint:prealloc

	// we use bytes instead of shares for tx and evd as they are encoded
	// contiguously in the square, unlike msgs where each of which is assigned their
	// own set of shares
	txBytes, evdBytes := 0, 0
	for _, pTx := range txs {
		// if there is no wire message in this tx, then we can simply add the
		// bytes and move on.
		if pTx.msg == nil {
			txBytes += len(pTx.rawTx)
			continue
		}

		// if there is a malleated tx, then we want to also account for the
		// txs that get included on-chain. The formula used here over
		// compensates for the actual size of the message, and in some cases can
		// result in some wasted square space or picking a square size that is
		// too large. TODO: improve by making a more accurate estimation formula
		txBytes += overEstimateMalleatedTxSize(len(pTx.rawTx), len(pTx.msg.Message), len(pTx.msg.MessageShareCommitment))

		msgSummaries = append(msgSummaries, msgSummary{shares.MsgSharesUsed(int(pTx.msg.MessageSize)), pTx.msg.MessageNamespaceId})
	}

	txShares = txBytes / appconsts.ContinuationCompactShareContentSize
	if txBytes > 0 {
		txShares++ // add one to round up
	}
	// todo: stop rounding up. Here we're rounding up because the calculation for
	// tx bytes isn't perfect. This catches those edge cases where we
	// estimate the exact number of shares in the square, when in reality we're
	// one byte over the number of shares in the square size. This will also cause
	// blocks that are one square size too big instead of being perfectly snug.
	// The estimation must be perfect or greater than what the square actually
	// ends up being.
	if txShares > 0 {
		txShares++
	}

	for _, e := range evd.Evidence {
		evdBytes += e.Size() + shares.DelimLen(uint64(e.Size()))
	}

	evdShares = evdBytes / appconsts.ContinuationCompactShareContentSize
	if evdBytes > 0 {
		evdShares++ // add one to round up
	}

	// sort the msgSummaries by namespace to order them properly. This is okay to do here
	// as we aren't sorting the actual txs, just their summaries for more
	// accurate estimations
	sort.Slice(msgSummaries, func(i, j int) bool {
		return bytes.Compare(msgSummaries[i].namespace, msgSummaries[j].namespace) < 0
	})

	// isolate the sizes as we no longer need the namespaces
	msgShares := make([]int, len(msgSummaries))
	for i, summary := range msgSummaries {
		msgShares[i] = summary.size
	}
	return txShares, evdShares, msgShares
}

// overEstimateMalleatedTxSize estimates the size of a malleated tx. The formula it uses will always over estimate.
func overEstimateMalleatedTxSize(txLen, msgLen, sharesCommitments int) int {
	// the malleated tx uses the original txLen to account for meta data from
	// the original tx, but removes the message and extra share commitments that
	// are in the wire message by subtracting msgLen and all extra share
	// commitments.
	malleatedTxLen := txLen - msgLen - ((sharesCommitments - 1) * appconsts.ShareCommitmentBytes)
	// we need to ensure that the returned number is at least larger than or
	// equal to the actual number, which is difficult to calculate without
	// actually malleating the tx
	return appconsts.MalleatedTxBytes + appconsts.MalleatedTxEstimateBuffer + malleatedTxLen
}
