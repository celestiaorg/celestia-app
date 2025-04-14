package app

import (
	"fmt"

	"cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"

	blobtx "github.com/celestiaorg/go-square/v2/tx"

	apperr "github.com/celestiaorg/celestia-app/v4/app/errors"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. This
// method wraps the default Baseapp's method so that it can parse and check
// transactions that contain blobs.
func (app *App) CheckTx(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	tx := req.Tx

	// all txs must be less than or equal to the max tx size limit
	maxTxSize := appconsts.DefaultMaxTxSize
	currentTxSize := len(tx)
	if currentTxSize > maxTxSize {
		return responseCheckTxWithEvents(errors.Wrapf(apperr.ErrTxExceedsMaxSize, "tx size %d bytes is larger than the application's configured MaxTxSize of %d bytes for version %d", currentTxSize, maxTxSize, appconsts.LatestVersion), 0, 0, []abci.Event{}, false), nil
	}

	// check if the transaction contains blobs
	btx, isBlob, err := blobtx.UnmarshalBlobTx(tx)
	if isBlob && err != nil {
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	if !isBlob {
		// reject transactions that can't be decoded
		sdkTx, err := app.encodingConfig.TxConfig.TxDecoder()(tx)
		if err != nil {
			return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
		}
		// reject transactions that have a MsgPFB but no blobs attached to the tx
		for _, msg := range sdkTx.GetMsgs() {
			if _, ok := msg.(*blobtypes.MsgPayForBlobs); !ok {
				continue
			}
			return responseCheckTxWithEvents(blobtypes.ErrNoBlobs, 0, 0, []abci.Event{}, false), nil
		}
		// don't do anything special if we have a normal transaction
		return app.BaseApp.CheckTx(req)
	}

	switch req.Type {
	// new transactions must be checked in their entirety
	case abci.CheckTxType_New:
		err = blobtypes.ValidateBlobTx(app.encodingConfig.TxConfig, btx, appconsts.SubtreeRootThreshold, appconsts.LatestVersion)
		if err != nil {
			return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
		}
	case abci.CheckTxType_Recheck:
	default:
		panic(fmt.Sprintf("unknown RequestCheckTx type: %s", req.Type))
	}

	// NOTE: we recreate the reqCheckTx such that we do not mutate the original req.Tx value
	return app.BaseApp.CheckTx(&abci.RequestCheckTx{
		Tx:   btx.Tx,
		Type: req.GetType(),
	})
}

func responseCheckTxWithEvents(err error, gw, gu uint64, events []abci.Event, debug bool) *abci.ResponseCheckTx {
	space, code, log := errors.ABCIInfo(err, debug)
	return &abci.ResponseCheckTx{
		Codespace: space,
		Code:      code,
		Log:       log,
		GasWanted: int64(gw),
		GasUsed:   int64(gu),
		Events:    events,
	}
}
