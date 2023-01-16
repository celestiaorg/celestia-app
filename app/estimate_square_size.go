package app

import (
	"encoding/binary"
	"math"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// estimateSquareSize uses the provided block data to over estimate the square
// size and the starting share index of non-reserved namespaces. The estimates
// returned are liberal in the sense that we assume close to worst case and
// round up.
//
// NOTE: The estimation process does not have to be perfect. We can overestimate
// because the cost of padding is limited.
func estimateSquareSize(txs []parsedTx) (squareSize uint64, nonreserveStart int) {
	txSharesUsed := estimateTxShares(appconsts.DefaultMaxSquareSize, txs)
	blobSharesUsed := 0

	for _, ptx := range txs {
		if len(ptx.normalTx) != 0 {
			continue
		}
		blobSharesUsed += blobtypes.BlobTxSharesUsed(ptx.blobTx)
	}

	// assume that we have to add a lot of padding by simply doubling the number
	// of shares used
	//
	// TODO: use a more precise estimation that doesn't over
	// estimate as much
	totalSharesUsed := uint64(txSharesUsed + blobSharesUsed)
	totalSharesUsed *= 2
	minSize := uint64(math.Sqrt(float64(totalSharesUsed)))
	squareSize = shares.RoundUpPowerOfTwo(minSize)
	if squareSize >= appconsts.DefaultMaxSquareSize {
		squareSize = appconsts.DefaultMaxSquareSize
	}
	if squareSize <= appconsts.DefaultMinSquareSize {
		squareSize = appconsts.DefaultMinSquareSize
	}

	return squareSize, txSharesUsed
}

// estimateTxShares estimates the number of shares used by transactions.
func estimateTxShares(squareSize uint64, ptxs []parsedTx) int {
	maxWTxOverhead := maxIndexWrapperOverhead(squareSize)
	maxIndexOverhead := maxIndexOverhead(squareSize)
	txbytes := 0
	for _, pTx := range ptxs {
		if len(pTx.normalTx) != 0 {
			txLen := len(pTx.normalTx)
			txLen += shares.DelimLen(uint64(txLen))
			txbytes += txLen
			continue
		}
		txLen := len(pTx.blobTx.Tx) + maxWTxOverhead + (maxIndexOverhead * len(pTx.blobTx.Blobs))
		txLen += shares.DelimLen(uint64(txLen))
		txbytes += txLen
	}

	return shares.CompactSharesNeeded(txbytes)
}

// maxWrappedTxOverhead calculates the maximum amount of overhead introduced by
// wrapping a transaction with a shares index
//
// TODO: make more efficient by only generating these numbers once or something
// similar. This function alone can take up to 5ms.
func maxIndexWrapperOverhead(squareSize uint64) int {
	maxTxLen := squareSize * squareSize * appconsts.ContinuationCompactShareContentSize
	wtx, err := coretypes.MarshalIndexWrapper(
		make([]byte, maxTxLen),
		uint32(squareSize*squareSize),
	)
	if err != nil {
		panic(err)
	}
	return len(wtx) - int(maxTxLen)
}

// maxIndexOverhead calculates the maximum amount of overhead in bytes that
// could occur by adding an index to an IndexWrapper.
func maxIndexOverhead(squareSize uint64) int {
	maxShareIndex := squareSize * squareSize
	maxIndexLen := binary.PutUvarint(make([]byte, binary.MaxVarintLen32), maxShareIndex)
	wtx, err := coretypes.MarshalIndexWrapper(make([]byte, 1), uint32(maxShareIndex))
	if err != nil {
		panic(err)
	}
	wtx2, err := coretypes.MarshalIndexWrapper(make([]byte, 1), uint32(maxShareIndex), uint32(maxShareIndex-1))
	if err != nil {
		panic(err)
	}
	return len(wtx2) - len(wtx) + maxIndexLen
}
