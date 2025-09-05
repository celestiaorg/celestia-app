package app

import (
	"fmt"

	"cosmossdk.io/errors"
	apperr "github.com/celestiaorg/celestia-app/v6/app/errors"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	blobtx "github.com/celestiaorg/go-square/v2/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
)

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. This
// method wraps the default Baseapp's method so that it can parse and check
// transactions that contain blobs.
func (app *App) CheckTx(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	tx := req.Tx

	// all txs must be less than or equal to the max tx size limit
	maxTxSize := appconsts.MaxTxSize
	currentTxSize := len(tx)
	if currentTxSize > maxTxSize {
		return responseCheckTxWithEvents(errors.Wrapf(apperr.ErrTxExceedsMaxSize, "tx size %d bytes is larger than the application's configured MaxTxSize of %d bytes for version %d", currentTxSize, maxTxSize, appconsts.Version), 0, 0, []abci.Event{}, false), nil
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
		resp, err := app.BaseApp.CheckTx(req)
		if err != nil {
			return nil, err
		}

		resp.Address, resp.Sequence, err = getSignersAndSequence(sdkTx)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	switch req.Type {
	// new transactions must be checked in their entirety
	case abci.CheckTxType_New:
		err = blobtypes.ValidateBlobTx(app.encodingConfig.TxConfig, btx, appconsts.SubtreeRootThreshold, appconsts.Version)
		if err != nil {
			return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
		}
	case abci.CheckTxType_Recheck:
	default:
		panic(fmt.Sprintf("unknown RequestCheckTx type: %s", req.Type))
	}

	// NOTE: we recreate the reqCheckTx such that we do not mutate the original req.Tx value
	resp, err := app.BaseApp.CheckTx(&abci.RequestCheckTx{
		Tx:   btx.Tx,
		Type: req.GetType(),
	})
	if err != nil {
		return nil, err
	}

	// these should not error as they should have already been evaluated in check tx
	sdkTx, err := app.encodingConfig.TxConfig.TxDecoder()(tx)
	if err != nil {
		return nil, err
	}

	resp.Address, resp.Sequence, err = getSignersAndSequence(sdkTx)
	if err != nil {
		return nil, err
	}

	return resp, nil
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

func getSignersAndSequence(sdkTx sdktypes.Tx) ([]byte, uint64, error) {
	sigTx, ok := sdkTx.(authsigning.Tx)
	if !ok {
		return nil, 0, sdkerrors.ErrTxDecode
	}

	signers, err := sigTx.GetSigners()
	if err != nil {
		return nil, 0, err
	}

	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return nil, 0, err
	}

	return signers[0], sigs[0].Sequence, nil
}