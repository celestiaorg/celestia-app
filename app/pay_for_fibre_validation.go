package app

import (
	"crypto/sha256"

	sdkerrors "cosmossdk.io/errors"
	apperr "github.com/celestiaorg/celestia-app/v10/app/errors"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// extractPayForFibre returns the tx's only MsgPayForFibre, if present.
func extractPayForFibre(tx sdk.Tx) (*fibretypes.MsgPayForFibre, error) {
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return nil, nil
	}

	var pff *fibretypes.MsgPayForFibre
	for _, msg := range msgs {
		if m, ok := msg.(*fibretypes.MsgPayForFibre); ok {
			pff = m
		}
	}

	if pff != nil && len(msgs) != 1 {
		return nil, sdkerrors.Wrapf(apperr.ErrInvalidPayForFibreTx, "tx contains a MsgPayForFibre and %d total messages", len(msgs))
	}
	return pff, nil
}

func pffSigCacheKey(tx []byte) [sha256.Size]byte {
	return sha256.Sum256(tx)
}

func (app *App) isPFFSigCached(tx []byte) bool {
	_, ok := app.pffSignatureVerificationCache.Load(pffSigCacheKey(tx))
	return ok
}

func (app *App) cachePFFSig(tx []byte) {
	app.pffSignatureVerificationCache.Store(pffSigCacheKey(tx), struct{}{})
}
