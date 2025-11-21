package app

import (
	"fmt"
	"time"

	"cosmossdk.io/errors"
	apperr "github.com/celestiaorg/celestia-app/v6/app/errors"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	blobtx "github.com/celestiaorg/go-square/v3/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. This
// method wraps the default Baseapp's method so that it can parse and check
// transactions that contain blobs.
func (app *App) CheckTx(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	s := time.Now()
	defer func() {
		fmt.Println("CheckTx took ", time.Since(s))
	}()
	app.checkStateMu.Lock()
	defer app.checkStateMu.Unlock()

	tx := req.Tx

	// all txs must be less than or equal to the max tx size limit
	maxTxSize := appconsts.MaxTxSize
	if len(tx) > maxTxSize {
		return responseCheckTxWithEvents(errors.Wrapf(apperr.ErrTxExceedsMaxSize, "tx size %d bytes is larger than the application's configured MaxTxSize of %d bytes for version %d", len(tx), maxTxSize, appconsts.Version), 0, 0, []abci.Event{}, false), nil
	}

	btx, isBlob, err := blobtx.UnmarshalBlobTx(tx)
	if isBlob && err != nil {
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	if isBlob {
		return app.handleBlobCheckTx(req, btx)
	}

	sdkTx, err := app.encodingConfig.TxConfig.TxDecoder()(tx)
	if err != nil {
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	for _, msg := range sdkTx.GetMsgs() {
		if _, ok := msg.(*blobtypes.MsgPayForBlobs); ok {
			return responseCheckTxWithEvents(blobtypes.ErrNoBlobs, 0, 0, []abci.Event{}, false), nil
		}
	}

	return app.forwardCheckTx(req, sdkTx)
}

func (app *App) handleBlobCheckTx(req *abci.RequestCheckTx, btx *blobtx.BlobTx) (*abci.ResponseCheckTx, error) {
	baseReq := &abci.RequestCheckTx{Tx: btx.Tx, Type: req.GetType()}

	switch req.Type {
	case abci.CheckTxType_New:
		if err := blobtypes.ValidateBlobTx(app.encodingConfig.TxConfig, btx, appconsts.SubtreeRootThreshold, appconsts.Version); err != nil {
			return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
		}
		// Cache the tx, so ProcessProposal will skip the validation step
		app.txCache.Set(btx.Tx)
	case abci.CheckTxType_Recheck:
		// no need to re-validate a blob
	default:
		panic(fmt.Sprintf("unknown RequestCheckTx type: %s", req.Type))
	}

	sdkTx, err := app.encodingConfig.TxConfig.TxDecoder()(baseReq.Tx)
	if err != nil {
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	return app.forwardCheckTx(baseReq, sdkTx)
}

func (app *App) forwardCheckTx(req *abci.RequestCheckTx, sdkTx sdk.Tx) (*abci.ResponseCheckTx, error) {
	res, err := app.BaseApp.CheckTx(req)
	if err != nil {
		return res, err
	}

	signerAddr, signerSeq, err := signerDataFromTx(sdkTx)
	if err != nil {
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	res.Address = signerAddr
	res.Sequence = signerSeq
	return res, nil
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

func signerDataFromTx(tx sdk.Tx) ([]byte, uint64, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return nil, 0, fmt.Errorf("tx of type %T does not implement SigVerifiableTx", tx)
	}

	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return nil, 0, err
	}

	if len(sigs) == 0 {
		return nil, 0, fmt.Errorf("tx of type %T contains no signatures", tx)
	}

	if sigs[0].PubKey == nil {
		return nil, 0, fmt.Errorf("tx signer %d has no associated pubkey", 0)
	}

	return sigs[0].PubKey.Address().Bytes(), sigs[0].Sequence, nil
}
