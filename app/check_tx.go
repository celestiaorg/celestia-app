package app

import (
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	abci "github.com/tendermint/tendermint/abci/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. This
// method wraps the default Baseapp's method so that it can parse and check
// transactions that contain blobs.
func (app *App) CheckTx(req abci.RequestCheckTx) abci.ResponseCheckTx {
	// check if the transaction contains blobs
	btx, isBlob := coretypes.UnmarshalBlobTx(req.Tx)

	// new transactions must be checked in their entirety
	if req.Type == abci.CheckTxType_New {
		if isBlob {
			// if the transaction contains blobs, validate the blob tx
			err := blobtypes.ValidateBlobTx(app.txConfig, btx)
			if err != nil {
				return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false)
			}
			// set the blobs tx to pass through the normal check tx
			req.Tx = btx.Tx
		} else {
			// reject transactions that can't be decoded
			sdkTx, err := app.txConfig.TxDecoder()(req.Tx)
			if err != nil {
				return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false)
			}
			// reject transactions that have a MsgPFB but no blobs attached to the tx
			for _, msg := range sdkTx.GetMsgs() {
				if _, ok := msg.(*blobtypes.MsgPayForBlobs); ok {
					return sdkerrors.ResponseCheckTxWithEvents(blobtypes.ErrNoBlobs, 0, 0, []abci.Event{}, false)
				}
			}
		}
	}
	// rechecked transactions don't need any added validity checks

	return app.BaseApp.CheckTx(req)
}
