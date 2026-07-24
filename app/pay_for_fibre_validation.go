package app

import (
	"crypto/sha256"

	fibrekeeper "github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func pffSignatureCacheKey(tx []byte) [sha256.Size]byte {
	return sha256.Sum256(tx)
}

func (app *App) hasPayForFibreSignatures(tx []byte) bool {
	_, ok := app.pffSignatureCache.Load(pffSignatureCacheKey(tx))
	return ok
}

func (app *App) cachePayForFibreSignatures(tx []byte) {
	app.pffSignatureCache.Store(pffSignatureCacheKey(tx), struct{}{})
}

// validatePayForFibreSignatures verifies on a cache miss. ProcessProposal must
// retain the fallback because proposal transactions may bypass local CheckTx.
func (app *App) validatePayForFibreSignatures(ctx sdk.Context, rawTx []byte, msg *fibretypes.MsgPayForFibre) (bool, error) {
	if app.hasPayForFibreSignatures(rawTx) {
		return true, nil
	}
	if err := app.FibreKeeper.ValidatePayForFibreSignatures(ctx, msg); err != nil {
		return false, err
	}
	return false, nil
}

func (app *App) validateAndApplyPayForFibre(ctx sdk.Context, rawTx []byte, msg *fibretypes.MsgPayForFibre) error {
	cached, err := app.validatePayForFibreSignatures(ctx, rawTx, msg)
	if err != nil {
		return err
	}
	msgServer := fibrekeeper.NewMsgServerImpl(*app.FibreKeeper)
	_, err = msgServer.PayForFibre(ctx, msg)
	if err == nil && !cached {
		app.cachePayForFibreSignatures(rawTx)
	}
	return err
}

// validateAndApplyFibreProposalTx keeps proposal-local Fibre state aligned
// with FinalizeBlock so later PFFs observe earlier payments and timeouts.
func (app *App) validateAndApplyFibreProposalTx(ctx sdk.Context, rawTx []byte, tx sdk.Tx) error {
	msgServer := fibrekeeper.NewMsgServerImpl(*app.FibreKeeper)
	for _, msg := range tx.GetMsgs() {
		switch msg := msg.(type) {
		case *fibretypes.MsgPayForFibre:
			if err := app.validateAndApplyPayForFibre(ctx, rawTx, msg); err != nil {
				return err
			}
		case *fibretypes.MsgPaymentPromiseTimeout:
			if _, err := msgServer.PaymentPromiseTimeout(ctx, msg); err != nil {
				return err
			}
		}
	}
	return nil
}
