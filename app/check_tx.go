package app

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/x/blob/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	abci "github.com/tendermint/tendermint/abci/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. This
// method wraps the default Baseapp's method so that we can handle the parsing
// and checking of blob containing transactions
func (app *App) CheckTx(req abci.RequestCheckTx) abci.ResponseCheckTx {
	tx := req.Tx
	// check if the transaction contains blobs
	btx, isBlob := coretypes.UnmarshalBlobTx(tx)

	// don't do anything special if we have a normal transactions
	if !isBlob {
		return app.BaseApp.CheckTx(req)
	}

	switch req.Type {
	// new transactions must be checked in their entirety
	case abci.CheckTxType_New:
		pBTx, err := types.ProcessBlobTx(app.txConfig, btx)
		if err != nil {
			return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false)
		}
		tx = pBTx.Tx
	case abci.CheckTxType_Recheck:
		// only replace the current transaction with the unwrapped one, as we
		// have already performed the necessary check for new transactions
		tx = btx.Tx
	default:
		panic(fmt.Sprintf("unknown RequestCheckTx type: %s", req.Type))
	}

	req.Tx = tx
	return app.BaseApp.CheckTx(req)
}
