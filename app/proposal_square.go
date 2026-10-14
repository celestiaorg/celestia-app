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

type proposalSquareResult struct {
	size  uint64
	root  []byte
	stage string
	err   error
}

// buildProposalSquare computes the exact square and DAH ProcessProposal checks,
// reading tx bytes and decoded views only. A panic is returned rather than
// crashing the node; a build cancelled between stages returns a zero result.
func (app *App) buildProposalSquare(txs [][]byte, blobTxs []*blobtx.BlobTx, decoded []*fibretypes.DecodedPayForFibre, maxSquareSize int, expectedSize uint64, cancel *atomic.Bool) (result proposalSquareResult) {
	defer func() {
		if r := recover(); r != nil {
			result.stage = "panic while building proposal square"
			result.err = fmt.Errorf("%v", r)
		}
	}()

	// go-square no longer decodes Cosmos SDK transactions itself.
	classifiedTxs, err := fibretypes.ClassifyTxsDecoded(txs, blobTxs, decoded)
	if err != nil {
		result.stage, result.err = "failed to classify transactions:", err
		return result
	}
	if cancel.Load() {
		return result
	}
	dataSquare, err := squarev4.Construct(classifiedTxs, maxSquareSize, appconsts.SubtreeRootThreshold)
	if err != nil {
		result.stage, result.err = "failed to build data square:", err
		return result
	}
	if cancel.Load() {
		return result
	}
	eds, err := da.ExtendSharesWithTreePool(share.ToBytes(dataSquare), app.TreePool())
	if err != nil {
		result.stage, result.err = "failure to compute extended data square from transactions:", err
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
		result.stage, result.err = "failure to create new data availability header", err
		return result
	}
	result.root = dah.Hash()
	return result
}
