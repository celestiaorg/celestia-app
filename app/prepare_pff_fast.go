package app

import (
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// preparePFFFast validates an all-PFF candidate on one disposable branch. If
// any transaction fails, the caller discards the branch and uses normal
// per-transaction filtering. PrepareProposal returns after this validation, so
// there is no state consumer that needs the branch's writes merged into ctx.
func (app *App) preparePFFFast(ctx sdk.Context, rawTxs [][]byte, decoded []sdk.Tx, handler sdk.AnteHandler, waitPFF func(int)) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	branch, _ := ctx.CacheContext()
	for i, rawTx := range rawTxs {
		if len(rawTx) > appconsts.MaxTxSize {
			return false
		}
		waitPFF(i)
		sdkTx := decoded[i]
		if err := validatePayForFibreTxShape(sdkTx); err != nil {
			return false
		}
		msg, isPFF := payForFibreMsg(sdkTx)
		if !isPFF {
			return false
		}
		if _, isBlob, _ := unmarshalBlobTxIfPresent(rawTx); isBlob {
			return false
		}
		branch = branch.WithTxBytes(rawTx).WithEventManager(sdk.NewEventManager())
		branch, err := handler(branch, sdkTx, false)
		if err != nil {
			return false
		}
		if err := executeProposalPFF(branch, msg, app.MsgServiceRouter()); err != nil {
			return false
		}
	}
	return true
}
