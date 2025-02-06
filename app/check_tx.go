package app

import (
	"fmt"

	"cosmossdk.io/errors"

	apperr "github.com/celestiaorg/celestia-app/v3/app/errors"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	blobtx "github.com/celestiaorg/go-square/v2/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. This
// method wraps the default Baseapp's method so that it can parse and check
// transactions that contain blobs.
func (app *App) CheckTx(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	tx := req.Tx

	// all txs must be less than or equal to the max tx size limit
	maxTxSize := appconsts.MaxTxSize(app.AppVersion())
	currentTxSize := len(tx)
	if currentTxSize > maxTxSize {
		err := errors.Wrapf(apperr.ErrTxExceedsMaxSize, "tx size %d bytes is larger than the application's configured MaxTxSize of %d bytes for version %d", currentTxSize, maxTxSize, app.AppVersion())
		return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	// check if the transaction contains blobs
	btx, isBlob, err := blobtx.UnmarshalBlobTx(tx)
	if isBlob && err != nil {
		return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	if !isBlob {
		// reject transactions that can't be decoded
		sdkTx, err := app.txConfig.TxDecoder()(tx)
		if err != nil {
			return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
		}
		// reject transactions that have a MsgPFB but no blobs attached to the tx
		for _, msg := range sdkTx.GetMsgs() {
			if _, ok := msg.(*blobtypes.MsgPayForBlobs); !ok {
				continue
			}
			return sdkerrors.ResponseCheckTxWithEvents(blobtypes.ErrNoBlobs, 0, 0, []abci.Event{}, false), blobtypes.ErrNoBlobs
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
			return sdkerrors.ResponseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
		}
	case abci.CheckTxType_Recheck:
	default:
		panic(fmt.Sprintf("unknown RequestCheckTx type: %s", req.Type))
	}

	req.Tx = btx.Tx
	return app.BaseApp.CheckTx(req)
}
