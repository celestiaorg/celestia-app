package app

import (
	"encoding/binary"
	"math"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// estimateSquareSize uses the provided block data to over estimate the square
// size and the starting share index of non-reserved namespaces. The estimates
// returned are liberal in the sense that we assume close to worst case and
// round up.
//
// NOTE: The estimation process does not have to be perfect. We can overestimate
// because the cost of padding is limited.
func estimateSquareSize(normalTxs [][]byte, blobTxs []core.BlobTx) (squareSize uint64, nonreserveStart int) {
	txSharesUsed := estimateTxSharesUsed(normalTxs)
	pfbTxSharesUsed := estimatePFBTxSharesUsed(appconsts.DefaultMaxSquareSize, blobTxs)
	blobSharesUsed := 0

	for _, blobTx := range blobTxs {
		blobSharesUsed += blobtypes.BlobTxSharesUsed(blobTx)
	}

	// assume that we have to add a lot of padding by simply doubling the number
	// of shares used
	//
	// TODO: use a more precise estimation that doesn't over
	// estimate as much
	totalSharesUsed := uint64(txSharesUsed + pfbTxSharesUsed + blobSharesUsed)
	totalSharesUsed *= 2
	minSize := uint64(math.Ceil(math.Sqrt(float64(totalSharesUsed))))
	squareSize = shares.RoundUpPowerOfTwo(minSize)
	if squareSize >= appconsts.DefaultMaxSquareSize {
		squareSize = appconsts.DefaultMaxSquareSize
	}
	if squareSize <= appconsts.DefaultMinSquareSize {
		squareSize = appconsts.DefaultMinSquareSize
	}

	return squareSize, txSharesUsed + pfbTxSharesUsed
}

// estimateTxSharesUsed estimates the number of shares used by ordinary
// transactions (i.e. all transactions that aren't PFBs).
func estimateTxSharesUsed(normalTxs [][]byte) int {
	txBytes := 0
	for _, tx := range normalTxs {
		txBytes += len(tx)
		txBytes += shares.DelimLen(uint64(len(tx)))
	}
	return shares.CompactSharesNeeded(txBytes)
}

// estimatePFBTxSharesUsed estimates the number of shares used by PFB
// transactions.
func estimatePFBTxSharesUsed(squareSize uint64, blobTxs []core.BlobTx) int {
	maxWTxOverhead := maxIndexWrapperOverhead(squareSize)
	maxIndexOverhead := maxIndexOverhead(squareSize)
	numBytes := 0
	for _, blobTx := range blobTxs {
		txLen := len(blobTx.Tx) + maxWTxOverhead + (maxIndexOverhead * len(blobTx.Blobs))
		txLen += shares.DelimLen(uint64(txLen))
		numBytes += txLen
	}
	return shares.CompactSharesNeeded(numBytes)
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
