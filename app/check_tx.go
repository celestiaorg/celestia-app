package app

import (
	"fmt"

	"cosmossdk.io/errors"
	apperr "github.com/celestiaorg/celestia-app/v11/app/errors"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. This
// method wraps the default Baseapp's method so that it can parse and check
// transactions that contain blobs.
func (app *App) CheckTx(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	app.checkStateMu.Lock()
	defer app.checkStateMu.Unlock()
	return app.checkTxLocked(req, nil)
}

// checkTxLocked is CheckTx under app.checkStateMu, on p's parallel worker branch if p.
func (app *App) checkTxLocked(req *abci.RequestCheckTx, p *parallelTx) (*abci.ResponseCheckTx, error) {
	if f := app.fastRecheck; f != nil && req.Type == abci.CheckTxType_Recheck {
		if f.shadow != nil {
			return f.shadowRecheck(req) // never on a worker: shadow mode rechecks sequentially
		}
		if res, ok := f.recheck(req.Tx, p); ok {
			return res, nil
		}
	}
	return app.checkTx(req, p)
}

// checkTx is CheckTx's stock path, under app.checkStateMu, on p's worker branch if p.
func (app *App) checkTx(req *abci.RequestCheckTx, p *parallelTx) (*abci.ResponseCheckTx, error) {
	tx := req.Tx

	// all txs must be less than or equal to the max tx size limit
	maxTxSize := appconsts.MaxTxSize
	if len(tx) > maxTxSize {
		return responseCheckTxWithEvents(errors.Wrapf(apperr.ErrTxExceedsMaxSize, "tx size %d bytes is larger than the application's configured MaxTxSize of %d bytes for version %d", len(tx), maxTxSize, appconsts.Version), 0, 0, []abci.Event{}, false), nil
	}

	// A PayForFibre recheck skips this wrapper: its checks only depend on bytes
	// CheckTx already admitted, and the mempool ignores signer/sequence on recheck.
	if req.Type == abci.CheckTxType_Recheck && pffDecodeCandidate(tx) {
		return app.recheckPayForFibre(req, p)
	}

	btx, isBlob, err := blobtx.UnmarshalBlobTxNoCopy(tx)
	if isBlob && err != nil {
		if errors.IsOf(err, blobtx.ErrNonCanonicalBlobTx, blobtx.ErrNestedBlobTx) {
			return responseCheckTxWithEvents(apperr.ErrNonCanonicalBlobTx, 0, 0, []abci.Event{}, false), nil
		}
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	if isBlob {
		return app.handleBlobCheckTx(req, btx, p)
	}

	sdkTx, err := app.decodeTx(tx, p)
	if err != nil {
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	for _, msg := range sdkTx.GetMsgs() {
		if _, ok := msg.(*blobtypes.MsgPayForBlobs); ok {
			return responseCheckTxWithEvents(blobtypes.ErrNoBlobs, 0, 0, []abci.Event{}, false), nil
		}
	}

	if msgCount := countExecutableMsgs(sdkTx.GetMsgs()); msgCount > appconsts.MaxSDKMessages {
		err := errors.Wrapf(apperr.ErrTxExceedsMaxSDKMessages, "tx contains %d messages, limit is %d", msgCount, appconsts.MaxSDKMessages)
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), nil
	}
	if proofCount := countZKProofMsgs(sdkTx.GetMsgs()); proofCount > appconsts.MaxZKProofMessages {
		err := errors.Wrapf(apperr.ErrTxExceedsMaxZKProofMessages, "tx contains %d proof-work messages, limit is %d", proofCount, appconsts.MaxZKProofMessages)
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), nil
	}
	if sigCount := countISMSignatures(sdkTx.GetMsgs()); sigCount > appconsts.MaxISMSignatures {
		err := errors.Wrapf(apperr.ErrTxExceedsMaxISMSignatures, "tx carries %d ISM signatures, limit is %d", sigCount, appconsts.MaxISMSignatures)
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), nil
	}

	// MsgPayForFibre must be the only message in its tx.
	if err := validatePayForFibreTxShape(sdkTx); err != nil {
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), nil
	}
	// A tx whose gas limit alone exceeds the block's SDK gas budget can never be
	// included. PayForFibre txs are bounded by their own per-block count instead.
	isPFF := fibretypes.PayForFibreMsg(sdkTx) != nil
	if !isPFF {
		if gas := txGasLimit(sdkTx); !fitsSDKGas(0, gas) {
			err := errors.Wrapf(apperr.ErrTxExceedsMaxSDKGas, "tx gas limit is %d, limit is %d", gas, appconsts.MaxSDKGas)
			return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), nil
		}
	}

	res, err := app.forwardCheckTx(req, sdkTx, p)
	if isPFF && req.Type == abci.CheckTxType_New && err == nil && res.Code == abci.CodeTypeOK {
		app.recheckTxs.add(tx, sdkTx)
	}
	return res, err
}

// recheckPayForFibre rechecks a PayForFibre on the decode cached at admission.
func (app *App) recheckPayForFibre(req *abci.RequestCheckTx, p *parallelTx) (*abci.ResponseCheckTx, error) {
	var sdkTx sdk.Tx
	cached := false
	if p != nil {
		sdkTx, cached = p.cached, p.cached != nil
	} else {
		sdkTx, cached = app.recheckTxs.get(req.Tx)
	}
	refill := false
	if !cached && (app.fastRecheck != nil || p != nil) {
		// With fast recheck, decode so the pass is recorded again and the decode kept.
		if dec, err := app.decodeTx(req.Tx, p); err == nil {
			sdkTx, refill = dec, app.fastRecheck != nil
		}
	}
	res, err := app.checkDecoded(req, sdkTx, p)
	switch {
	case err != nil || res.Code != abci.CodeTypeOK:
		if p != nil {
			p.drop = true
		} else {
			app.recheckTxs.drop(req.Tx)
		}
	case refill && p != nil:
		p.refill = sdkTx
	case refill:
		app.recheckTxs.add(req.Tx, sdkTx)
	}
	return res, err
}

// checkDecoded runs BaseApp's CheckTx (on p's branch, if p) with sdkTx, the
// decode of req.Tx or nil, and records an eligible passed tx for fast recheck.
func (app *App) checkDecoded(req *abci.RequestCheckTx, sdkTx sdk.Tx, p *parallelTx) (res *abci.ResponseCheckTx, err error) {
	if p != nil {
		return app.recheckOnBranch(req.Tx, sdkTx, p), nil
	}
	if sdkTx == nil {
		return app.BaseApp.CheckTx(req)
	}
	if f := app.fastRecheck; f != nil {
		if pre := f.peek(sdkTx); pre != nil {
			defer func() { f.record(req.Tx, pre, res, err) }()
		}
	}
	defer app.txHandoff.lend(req.Tx, sdkTx)()
	return app.BaseApp.CheckTx(req)
}

func (app *App) handleBlobCheckTx(req *abci.RequestCheckTx, btx *blobtx.BlobTx, p *parallelTx) (*abci.ResponseCheckTx, error) {
	baseReq := &abci.RequestCheckTx{Tx: btx.Tx, Type: req.GetType()}

	// Decode before ValidateBlobTx, which decodes without a recover.
	sdkTx, err := app.decodeTx(baseReq.Tx, p)
	if err != nil {
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	switch req.Type {
	case abci.CheckTxType_New:
		if err := blobtypes.ValidateBlobTx(app.encodingConfig.TxConfig, btx, appconsts.SubtreeRootThreshold, appconsts.Version); err != nil {
			return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
		}
	case abci.CheckTxType_Recheck:
		// no need to re-validate a blob
	default:
		err := fmt.Errorf("unknown RequestCheckTx type: %s", req.Type)
		return responseCheckTxWithEvents(err, 0, 0, []abci.Event{}, false), err
	}

	res, err := app.forwardCheckTx(baseReq, sdkTx, p)
	if err != nil || res.Code != abci.CodeTypeOK {
		return res, err
	}

	// Cache only txs that passed full CheckTx, so ProcessProposal skips
	// re-validation and invalid spam cannot evict legitimate entries.
	if req.Type == abci.CheckTxType_New {
		app.txCache.Set(btx.Tx, btx.Blobs)
	}

	return res, nil
}

func (app *App) forwardCheckTx(req *abci.RequestCheckTx, sdkTx sdk.Tx, p *parallelTx) (*abci.ResponseCheckTx, error) {
	res, err := app.checkDecoded(req, sdkTx, p)
	if err != nil {
		return res, err
	}

	// BaseApp.CheckTx reports a failed tx via the response code with a nil
	// error. A failed tx may be malformed in ways that make signerDataFromTx
	// panic (e.g. a SignerInfo with an unset ModeInfo oneof), so skip it and
	// return the failure as-is.
	if res.Code != abci.CodeTypeOK {
		return res, nil
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

func signerDataFromTx(tx sdk.Tx) (addr []byte, seq uint64, err error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return nil, 0, fmt.Errorf("tx of type %T does not implement SigVerifiableTx", tx)
	}

	// GetSignaturesV2 panics on a malformed SignerInfo (e.g. an unset ModeInfo
	// oneof). Recover so a bad tx returns an error rather than crashing the
	// process, which is not always running under a recover (e.g. the mempool).
	defer func() {
		if r := recover(); r != nil {
			addr, seq, err = nil, 0, fmt.Errorf("failed to get signer data: %v", r)
		}
	}()

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

// validatePayForFibreTxShape rejects txs that mix MsgPayForFibre with other
// messages, and txs whose payment promise fails stateless validation. Both
// CheckTx and ProcessProposal call this, so a promise that would fail
// ValidateBasic in FinalizeBlock never reaches the square.
func validatePayForFibreTxShape(tx sdk.Tx) error {
	msgs := tx.GetMsgs()
	for _, msg := range msgs {
		pff, isPFF := msg.(*fibretypes.MsgPayForFibre)
		if !isPFF {
			continue
		}
		if len(msgs) > 1 {
			return errors.Wrapf(apperr.ErrInvalidPayForFibreTx, "tx contains a MsgPayForFibre and %d total messages", len(msgs))
		}
		if err := pff.PaymentPromise.ValidateBasic(); err != nil {
			return errors.Wrapf(apperr.ErrInvalidPayForFibreTx, "invalid payment promise: %s", err)
		}
	}
	return nil
}

// containsFibreStateMsg reports whether the tx carries a fibre message that
// mutates escrow state/balance as soon as it executes, so proposal must replay
// it before settling a later MsgPayForFibre. MsgPayForFibre (settled on its
// own path) and MsgRequestWithdrawal (only locks funds, paid out later by the
// BeginBlocker) are excluded.
func containsFibreStateMsg(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *fibretypes.MsgPaymentPromiseTimeout,
			*fibretypes.MsgDepositToEscrow:
			return true
		}
	}
	return false
}

// checkTxDecoder returns the tx decoder with a recover, so a tx whose decoding
// panics is rejected like any other tx that fails to decode. The panic is
// logged at debug level because any peer can send such a tx.
func (app *App) checkTxDecoder() sdk.TxDecoder {
	return recoverTxDecoder(app.Logger().Debug, app.encodingConfig.TxConfig.TxDecoder())
}
