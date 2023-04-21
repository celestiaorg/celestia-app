package app

import (
	appversion "github.com/celestiaorg/celestia-app/x/version"
	abci "github.com/tendermint/tendermint/abci/types"
)

func (app *App) EndBlock(req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	res = app.BaseApp.EndBlock(req)
	ctx := app.GetContextForDeliverTx([]byte{1})
	return appversion.EndBlocker(ctx, app.VersionKeeper, res)
}
