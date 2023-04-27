package app

import (
	appversion "github.com/celestiaorg/celestia-app/x/version"
	abci "github.com/tendermint/tendermint/abci/types"
)

// EndBlock wraps the BaseApp's end block method. This is done to modify the app
// version if necessary.
func (app *App) EndBlock(req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	res = app.BaseApp.EndBlock(req)
	ctx := app.GetContextForDeliverTx([]byte{1})
	return appversion.EndBlocker(ctx, app.VersionKeeper, res)
}
