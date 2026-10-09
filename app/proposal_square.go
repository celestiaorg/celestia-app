package app

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/da"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	squarev4 "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
)

// minParallelSquareTxs is the smallest all-PayForFibre block whose square is built
// alongside the walk. A judgement, not a measurement: small blocks have little to hide.
const minParallelSquareTxs = 100

// errProposalSquareCancelled marks a build stopped between stages; its zero size and
// root must never reach the proposer's claims, which SquareSize 0 would match.
var errProposalSquareCancelled = errors.New("proposal square build cancelled")

type proposalSquareResult struct {
	size uint64
	root []byte
	err  error
}

// buildProposalSquare computes the square and DAH ProcessProposal checks from tx
// bytes and decoded views only; a panic or cancellation is returned as an error.
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
		return proposalSquareResult{err: errProposalSquareCancelled}
	}
	dataSquare, err := squarev4.Construct(classifiedTxs, maxSquareSize, appconsts.SubtreeRootThreshold)
	if err != nil {
		result.err = fmt.Errorf("failed to build data square: %w", err)
		return result
	}
	if cancel.Load() {
		return proposalSquareResult{err: errProposalSquareCancelled}
	}
	size, rows, columns, err := app.TreePool().ComputeRoots(share.ToBytes(dataSquare), expectedSize)
	if err != nil {
		result.err = fmt.Errorf("failure to compute data roots from transactions: %w", err)
		return result
	}
	result.size = size
	if result.size != expectedSize {
		return result
	}
	if cancel.Load() {
		return proposalSquareResult{err: errProposalSquareCancelled}
	}
	dah := da.DataAvailabilityHeader{RowRoots: rows, ColumnRoots: columns}
	result.root = dah.Hash()
	return result
}
