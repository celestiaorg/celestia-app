package app

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"
)

var _ abci.RecheckTxsApplication = (*App)(nil)

// RecheckTxs rechecks the mempool in one call, with the verdicts and check state of
// CheckTx(Type=Recheck) on each tx in order. A nil response keeps the tx, as a CheckTx error does.
func (app *App) RecheckTxs(_ context.Context, req *abci.RequestRecheckTxs) (*abci.ResponseRecheckTxs, error) {
	app.checkStateMu.Lock()
	defer app.checkStateMu.Unlock()
	resps := app.recheckParallel(req.Txs)
	if resps == nil {
		resps = make([]*abci.ResponseCheckTx, len(req.Txs))
		for i, tx := range req.Txs {
			if res, err := app.checkTxLocked(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_Recheck}, nil); err == nil {
				resps[i] = res
			}
		}
	}
	return &abci.ResponseRecheckTxs{Responses: resps}, nil
}
