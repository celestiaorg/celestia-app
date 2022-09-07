package app

import (
	"bytes"
	"math"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/tendermint/tendermint/pkg/consts"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// prune removes txs until the set of txs will fit in the square of size
// squareSize. It assumes that the currentShareCount is accurate. This function
// is not perfectly accurate becuse accurately knowing how many shares any give
// malleated tx and its message takes up in a data square that is following the
// non-interactive default rules requires recalculating the square.
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
		if contigBytesCursor >= consts.TxShareSize {
			removedContiguousShares += (contigBytesCursor / consts.TxShareSize)
			contigBytesCursor = contigBytesCursor % consts.TxShareSize
		}
	}

	for i := len(txs) - 1; (removedContiguousShares + removedMessageShares) < goal; i-- {
		removedTxs++
		if txs[i].msg == nil {
			adjustContigCursor(len(txs[i].rawTx))
			continue
		}

		removedMessageShares += shares.MsgSharesUsed(len(txs[i].msg.GetMessage()))
		// we ignore the error here, as if there is an error malleating the tx,
		// then it we need to remove it anyway and will not end up contributing
		// bytes to the square anyway.
		_ = txs[i].malleate(txConf, uint64(squareSize))
		adjustContigCursor(len(txs[i].malleatedTx) + appconsts.MalleatedTxBytes)
	}

	return txs[:len(txs)-(removedTxs)-1]
}

// calculateCompactShareCount calculates the exact number of compact shares used.
func calculateCompactShareCount(txs []*parsedTx, evd core.EvidenceList, squareSize int) int {
	txSplitter, evdSplitter := shares.NewContiguousShareSplitter(consts.TxNamespaceID), shares.NewContiguousShareSplitter(consts.EvidenceNamespaceID)
	var err error
	msgSharesCursor := len(txs)
	for _, tx := range txs {
		rawTx := tx.rawTx
		if tx.malleatedTx != nil {
			rawTx, err = coretypes.WrapMalleatedTx(tx.originalHash(), uint32(msgSharesCursor), tx.malleatedTx)
			// we should never get to this point, but just in case we do, we
			// catch the error here on purpose as we want to ignore txs that are
			// invalid (cannot be wrapped)
			if err != nil {
				continue
			}
			used, _ := shares.MsgSharesUsedNIDefaults(msgSharesCursor, squareSize, tx.msg.Size())
			msgSharesCursor += used
		}
		txSplitter.WriteTx(rawTx)
	}
	for _, e := range evd.Evidence {
		evidence, err := coretypes.EvidenceFromProto(&e)
		if err != nil {
			panic(err)
		}
		evdSplitter.WriteEvidence(evidence)
	}
	txCount, available := txSplitter.Count()
	if consts.TxShareSize-available > 0 {
		txCount++
	}
	evdCount, available := evdSplitter.Count()
	if consts.TxShareSize-available > 0 {
		evdCount++
	}
	return txCount + evdCount
}

// estimateSquareSize uses the provided block data to estimate the square size
// assuming that all malleated txs follow the non interactive default rules.
// todo: get rid of the second shares used int as its not used atm
func estimateSquareSize(txs []*parsedTx, evd core.EvidenceList) (uint64, int) {
	// get the raw count of shares taken by each type of block data
	txShares, evdShares, msgLens := rawShareCount(txs, evd)
	msgShares := 0
	for _, msgLen := range msgLens {
		msgShares += msgLen
	}

	// calculate the smallest possible square size that could contian all the
	// messages
	squareSize := nextPowerOfTwo(int(math.Ceil(math.Sqrt(float64(txShares + evdShares + msgShares)))))

	// the starting square size should be the minimum
	if squareSize < consts.MinSquareSize {
		squareSize = int(consts.MinSquareSize)
	}

	var fits bool
	for {
		// assume that all the msgs in the square use the non-interactive
		// default rules and see if we can fit them in the smallest starting
		// square size. We start the cusor (share index) at the begginning of
		// the message shares (txShares+evdShares), because shares that do not
		// follow the non-interactive defaults are simple to estimate.
		fits, msgShares = shares.FitsInSquare(txShares+evdShares, squareSize, msgLens...)
		switch {
		// stop estimating if we know we can reach the max square size
		case squareSize >= consts.MaxSquareSize:
			return consts.MaxSquareSize, txShares + evdShares + msgShares
		// return if we've found a square size that fits all of the txs
		case fits:
			return uint64(squareSize), txShares + evdShares + msgShares
		// try the next largest square size if we can't fit all the txs
		case !fits:
			// increment the square size
			squareSize = int(nextPowerOfTwo(squareSize + 1))
		}
	}
}

// rawShareCount calculates the number of shares taken by all of the included
// txs, evidence, and each msg.
func rawShareCount(txs []*parsedTx, evd core.EvidenceList) (txShares, evdShares int, msgLens []int) {
	// msgSummary is used to keep track fo the size and the namespace so that we
	// can sort the namespaces before returning.
	type msgSummary struct {
		size      int
		namespace []byte
	}

	var msgSummaries []msgSummary

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

		// if the there is a malleated tx, then we want to also account for the
		// txs that gets included onchain TODO: improve
		txBytes += calculateMalleatedTxSize(len(pTx.rawTx), len(pTx.msg.Message), len(pTx.msg.MessageShareCommitment))

		msgSummaries = append(msgSummaries, msgSummary{shares.MsgSharesUsed(int(pTx.msg.MessageSize)), pTx.msg.MessageNameSpaceId})
	}

	txShares = txBytes / consts.TxShareSize
	if txBytes > 0 {
		txShares++ // add one to round up
	}
	// todo: stop rounding up. Here we're rounding up because the calculation for
	// tx bytes isn't perfect. This catches those edge cases where we're we
	// estimate the exact number of shares in the square, when in reality we're
	// one over the number of shares in the square size. This will also cause
	// blocks that are one square size too big instead of being perfectly snug.
	// The estimation must be perfect or greater than what the square actually
	// ends up being.
	if txShares > 0 {
		txShares++
	}

	for _, e := range evd.Evidence {
		evdBytes += e.Size() + shares.DelimLen(uint64(e.Size()))
	}

	evdShares = evdBytes / consts.TxShareSize
	if evdBytes > 0 {
		evdShares++ // add one to round up
	}

	// sort the msgSummaries in order to order properly. This is okay to do here
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
	return txShares + 2, evdShares, msgShares
}

// todo: add test to make sure that we change this each time something changes from payForData
func calculateMalleatedTxSize(txLen, msgLen, sharesCommitments int) int {
	// the malleated tx uses meta data from the original tx, but removes the
	// message and extra share commitments. Only a single share commitment will
	// make it on chain, and the square size (uint64) is removed.
	malleatedTxLen := txLen - msgLen - ((sharesCommitments - 1) * 128) - 8
	// todo: fix majic number 100 here
	return appconsts.MalleatedTxBytes + 100 + malleatedTxLen
}

func nextPowerOfTwo(v int) int {
	k := 1
	for k < v {
		k = k << 1
	}
	return k
}
