package app

import (
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// preparePFFFast validates an all-PFF candidate on one disposable branch. If
// any transaction fails, the caller discards the branch and uses normal
// per-transaction filtering. The returned write function is called only after
// the candidate square has also been built successfully.
func (app *App) preparePFFFast(ctx sdk.Context, rawTxs [][]byte, decoded []sdk.Tx, handler sdk.AnteHandler, waitPFF func(int)) (write func(), ok bool) {
	defer func() {
		if recover() != nil {
			write, ok = nil, false
		}
	}()
	branch, commit := ctx.CacheContext()
	for i, rawTx := range rawTxs {
		waitPFF(i)
		sdkTx := decoded[i]
		if err := validatePayForFibreTxShape(sdkTx); err != nil {
			return nil, false
		}
		msg, isPFF := payForFibreMsg(sdkTx)
		if !isPFF {
			return nil, false
		}
		_, isFibre, err := fibretypes.TryParseFibreTx(rawTx)
		if err != nil || !isFibre {
			return nil, false
		}
		branch = branch.WithTxBytes(rawTx).WithEventManager(sdk.NewEventManager())
		branch, err = handler(branch, sdkTx, false)
		if err != nil {
			return nil, false
		}
		if err := executeProposalPFF(branch, msg, app.MsgServiceRouter()); err != nil {
			return nil, false
		}
	}
	return commit, true
}
