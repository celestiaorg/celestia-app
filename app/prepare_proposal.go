package app

import (
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/pkg/consts"
)

// PreprocessTxs fullfills the celestia-core version of the ACBI interface, by
// performing basic validation for the incoming txs, and by cleanly separating
// share messages from transactions
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {

	// todo(evan): create the DAH using the square
	_, data, err := WriteSquare(app.txConfig, app.SquareSize(), req.BlockData)
	if err != nil {
		// todo(evan): see if we can get rid of this panic
		panic(err)
	}

	return abci.ResponsePrepareProposal{
		BlockData: data,
	}
}

// SquareSize returns the current square size. Currently, the square size is
// hardcoded. todo(evan): don't hardcode the square size
func (app *App) SquareSize() uint64 {
	return consts.MaxSquareSize
}
