package app

import (
	"fmt"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/da"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
)

type proposalSquareResult struct {
	size  uint64
	root  []byte
	stage string
	err   error
}

// buildProposalSquare computes the exact square and DAH ProcessProposal checks.
// It reads transaction bytes and decoded views only; any panic is returned to
// the proposal handler so a background builder cannot crash the node.
//
// cancel is checked between stages. When the ordered walk has already rejected
// the proposal nothing will read the result, and each remaining stage - the
// square, the extended square, the data availability header - is expensive
// enough that finishing them would delay the rejection the walk already
// reached. A cancelled build returns a zero result, which the caller discards.
func (app *App) buildProposalSquare(txs [][]byte, blobTxs []*blobtx.BlobTx, decoded []*fibretypes.DecodedPayForFibre, maxSquareSize int, expectedSize uint64, cancel *atomic.Bool) (result proposalSquareResult) {
	defer func() {
		if r := recover(); r != nil {
			result.stage = "panic while building proposal square"
			result.err = fmt.Errorf("%v", r)
		}
	}()

	// go-square no longer decodes Cosmos SDK transactions itself.
	classifiedTxs, err := classifyTxs(txs, blobTxs, decoded)
	if err != nil {
		result.stage, result.err = "failed to classify transactions:", err
		return result
	}
	if cancel.Load() {
		return result
	}
	dataSquare, err := constructSquare(classifiedTxs, blobTxs, maxSquareSize, appconsts.SubtreeRootThreshold)
	if err != nil {
		result.stage, result.err = "failed to build data square:", err
		return result
	}
	if cancel.Load() {
		return result
	}
	size, rows, columns, err := app.TreePool().ComputeRoots(share.ToBytes(dataSquare), expectedSize)
	if err != nil {
		result.stage, result.err = "failure to compute data roots from transactions:", err
		return result
	}
	result.size = size
	if result.size != expectedSize {
		return result
	}
	if cancel.Load() {
		return result
	}
	dah := da.DataAvailabilityHeader{RowRoots: rows, ColumnRoots: columns}
	result.root = dah.Hash()
	return result
}
