package app

import (
	"fmt"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/da"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	squarev4 "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
)

// minParallelSquareTxs is the smallest all-PayForFibre block whose square
// ProcessProposal builds alongside the ordered walk. It keeps the goroutine and
// the dedup scan off small blocks, which have little square work to hide. The
// value is a judgement, not a measurement: the overlap was measured on a
// 5,800-PFF block, and with MaxPayForFibreMessages at 200 a default build takes
// it only from here to 200.
const minParallelSquareTxs = 100

type proposalSquareResult struct {
	size uint64
	root []byte
	err  error
}

// buildProposalSquare computes the exact square and DAH ProcessProposal checks,
// reading tx bytes and decoded views only. A panic is returned rather than
// crashing the node; a build cancelled between stages returns a zero result.
func (app *App) buildProposalSquare(txs [][]byte, blobTxs []*blobtx.BlobTx, decoded []*fibretypes.DecodedPayForFibre, maxSquareSize int, expectedSize uint64, cancel *atomic.Bool) (result proposalSquareResult) {
	defer func() {
		if r := recover(); r != nil {
			result.err = fmt.Errorf("panic while building proposal square: %v", r)
		}
	}()

	// go-square no longer decodes Cosmos SDK transactions itself.
	classifiedTxs, err := fibretypes.ClassifyTxsDecoded(txs, blobTxs, decoded)
	if err != nil {
		result.err = fmt.Errorf("failed to classify transactions: %w", err)
		return result
	}
	if cancel.Load() {
		return result
	}
	dataSquare, err := squarev4.Construct(classifiedTxs, maxSquareSize, appconsts.SubtreeRootThreshold)
	if err != nil {
		result.err = fmt.Errorf("failed to build data square: %w", err)
		return result
	}
	if cancel.Load() {
		return result
	}
	eds, err := da.ExtendSharesWithTreePool(share.ToBytes(dataSquare), app.TreePool())
	if err != nil {
		result.err = fmt.Errorf("failure to compute extended data square from transactions: %w", err)
		return result
	}
	result.size = uint64(eds.Width()) / 2
	if result.size != expectedSize {
		return result
	}
	if cancel.Load() {
		return result
	}
	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		result.err = fmt.Errorf("failure to create new data availability header: %w", err)
		return result
	}
	result.root = dah.Hash()
	return result
}
