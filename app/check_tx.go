package app

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	"github.com/celestiaorg/go-square/blob"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	abci "github.com/tendermint/tendermint/abci/types"
)

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. This
// method wraps the default Baseapp's method so that it can parse and check
// transactions that contain blobs.
func (app *App) CheckTx(req abci.RequestCheckTx) abci.ResponseCheckTx {
	tx := req.Tx
	// check if the transaction contains blobs
	btx, isBlob := blob.UnmarshalBlobTx(tx)

	if !isBlob {
		// reject transactions that can't be decoded
		sdkTx, err := app.txConfig.TxDecoder()(tx)
		if err != nil {
			return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false)
		}
		// reject transactions that have a MsgPFB but no blobs attached to the tx
		for _, msg := range sdkTx.GetMsgs() {
			if _, ok := msg.(*blobtypes.MsgPayForBlobs); !ok {
				continue
			}
			return sdkerrors.ResponseCheckTxWithEvents(blobtypes.ErrNoBlobs, 0, 0, []abci.Event{}, false)
		}
		// don't do anything special if we have a normal transaction
		return app.BaseApp.CheckTx(req)
	}

	switch req.Type {
	// new transactions must be checked in their entirety
	case abci.CheckTxType_New:
		// FIXME: we have a hardcoded subtree root threshold here. This is because we can't access
		// the app version because the context is not initialized
		err := blobtypes.ValidateBlobTx(app.txConfig, btx, appconsts.DefaultSubtreeRootThreshold)
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
