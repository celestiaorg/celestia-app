package app

import (
	"fmt"

	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
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

	if !isBlob {
		sdkTx, err := app.txConfig.TxDecoder()(tx)
		if err != nil {
			return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false)
		}
		// reject transactions that have PFBs, but no blobs attached
		for _, msg := range sdkTx.GetMsgs() {
			if _, ok := msg.(*blobtypes.MsgPayForBlob); !ok {
				continue
			}
			return sdkerrors.ResponseCheckTxWithEvents(blobtypes.ErrBloblessPFB, 0, 0, []abci.Event{}, false)
		}
		// don't do anything special if we have a normal transaction
		return app.BaseApp.CheckTx(req)
	}

	switch req.Type {
	// new transactions must be checked in their entirety
	case abci.CheckTxType_New:
		err := blobtypes.ValidateBlobTx(app.txConfig, btx)
		if err != nil {
			return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false)
		}
	case abci.CheckTxType_Recheck:
	default:
		panic(fmt.Sprintf("unknown RequestCheckTx type: %s", req.Type))
	}

	req.Tx = btx.Tx
	return app.BaseApp.CheckTx(req)
}
