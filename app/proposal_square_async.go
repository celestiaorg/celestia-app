package app

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/da"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
)

type proposalSquareResult struct {
	root         []byte
	stage        string
	err          error
	sizeMismatch bool
}

// computeProposalSquare is independent of ante state. Its result is used only
// after the serial ante and settlement pass has accepted every transaction.
func (app *App) computeProposalSquare(req *abci.RequestProcessProposal, maxSquareSize int, decodedMessages []*fibretypes.MsgPayForFibre, canceled <-chan struct{}) (result proposalSquareResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result.stage = "failure to compute data square"
			result.err = fmt.Errorf("caught panic: %v", recovered)
		}
	}()
	classified, err := fibretypes.ClassifyTxsForProposalWithMessages(req.Txs, decodedMessages)
	if err != nil {
		result.stage, result.err = "failed to classify transactions", err
		return
	}
	if proposalSquareCanceled(canceled) {
		return
	}
	dataSquare, err := square.Construct(classified, maxSquareSize, appconsts.SubtreeRootThreshold)
	if err != nil {
		result.stage, result.err = "failed to build data square", err
		return
	}
	if proposalSquareCanceled(canceled) {
		return
	}
	eds, err := da.ExtendSharesWithTreePool(share.ToBytes(dataSquare), app.TreePool())
	if err != nil {
		result.stage, result.err = "failure to compute extended data square from transactions", err
		return
	}
	if uint64(eds.Width())/2 != req.SquareSize {
		result.sizeMismatch = true
		return
	}
	if proposalSquareCanceled(canceled) {
		return
	}
	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		result.stage, result.err = "failure to create new data availability header", err
		return
	}
	result.root = dah.Hash()
	return
}

func proposalSquareCanceled(canceled <-chan struct{}) bool {
	select {
	case <-canceled:
		return true
	default:
		return false
	}
}
