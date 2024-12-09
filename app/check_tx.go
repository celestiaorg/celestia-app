package app

import (
	"cosmossdk.io/errors"
	"fmt"

	apperr "github.com/celestiaorg/celestia-app/v3/app/errors"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	blobtx "github.com/celestiaorg/go-square/v2/tx"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	abci "github.com/tendermint/tendermint/abci/types"
)

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. This
// method wraps the default Baseapp's method so that it can parse and check
// transactions that contain blobs.
func (app *App) CheckTx(req abci.RequestCheckTx) abci.ResponseCheckTx {
	tx := req.Tx

	// all txs must be less than or equal to the max tx size limit
	maxTxSize := appconsts.MaxTxSize(app.AppVersion())
	currentTxSize := len(tx)
	if currentTxSize > maxTxSize {
		return sdkerrors.ResponseCheckTxWithEvents(errors.Wrapf(apperr.ErrTxExceedsMaxSize, "tx size %d bytes is larger than the application's configured threshold of %d bytes", currentTxSize, maxTxSize), 0, 0, []abci.Event{}, false)
	}

	// check if the transaction contains blobs
	btx, isBlob, err := blobtx.UnmarshalBlobTx(tx)
	if isBlob && err != nil {
		return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false)
	}

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
		appVersion := app.AppVersion()
		err := blobtypes.ValidateBlobTx(app.txConfig, btx, appconsts.SubtreeRootThreshold(appVersion), appVersion)
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
