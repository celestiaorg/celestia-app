package app

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/da"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	squarev4 "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
)

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
		return proposalSquareResult{err: errProposalSquareCancelled}
	}
	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		result.err = fmt.Errorf("failure to create new data availability header: %w", err)
		return result
	}
	result.root = dah.Hash()
	return result
}
