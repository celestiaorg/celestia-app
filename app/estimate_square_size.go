package app

import (
	"math"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	coretypes "github.com/tendermint/tendermint/types"
)

// estimateSquareSize uses the provided block data to over estimate the square
// size and the starting share index of non-reserved namespaces. The estimates
// returned are liberal in the sense that we assume close to worst case and
// round up.
//
// NOTE: The estimation process does not have to be perfect. We can overestimate
// because the cost of padding TODO: cache and return the number of shares a
// blob uses so we don't recalculate it later.
func estimateSquareSize(txs []parsedTx) (squareSize uint64, nonreserveStart int) {
	txSharesUsed := compactSharesUsed(appconsts.MaxSquareSize, txs)
	msgSharesUsed := 0

	for _, ptx := range txs {
		if ptx.normalTx != nil {
			continue
		}
		msgSharesUsed += shares.MsgSharesUsed(ptx.blobTx.DataUsed())
	}

	// assume that we have to add a lot of padding by simply doubling the number
	// of shares used
	totalSharesUsed := txSharesUsed + msgSharesUsed
	totalSharesUsed *= 2

	return uint64(math.Sqrt(float64(totalSharesUsed))), txSharesUsed
}

// compactSharesUsed calculates the amount of shares used by the celestia
// specific transactions
func compactSharesUsed(squareSize uint64, ptxs []parsedTx) int {
	maxWTxOverhead := maxWrappedTxOverhead(squareSize)
	txbytes := 0
	for _, pTx := range ptxs {
		if pTx.normalTx == nil {
			txbytes += len(pTx.normalTx)
			continue
		}
		txbytes += len(pTx.blobTx.Tx) + maxWTxOverhead
	}

	sharesUsed := 1
	if txbytes <= appconsts.FirstCompactShareContentSize {
		return sharesUsed
	}

	// account for the first share
	txbytes -= appconsts.FirstCompactShareContentSize
	sharesUsed += (txbytes / appconsts.ContinuationCompactShareContentSize) + 1 // add 1 to round up and another to account for the first share

	return sharesUsed
}

// maxWrappedTxOverhead calculates the maximum amount of overhead introduced by
// wrapping a transaction with a shares index
func maxWrappedTxOverhead(squareSize uint64) int {
	tx := []byte{1}
	wtx, err := coretypes.WrapMalleatedTx(uint32(squareSize*squareSize), tx)
	if err != nil {
		panic(err)
	}
	return len(wtx) - len(tx)
}
