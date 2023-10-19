package app

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/x/upgrade"
	abci "github.com/tendermint/tendermint/abci/types"
)

func (app *App) DeliverTx(req abci.RequestDeliverTx) abci.ResponseDeliverTx {
	sdkTx, err := app.txConfig.TxDecoder()(req.Tx)
	if err == nil {
		if appVersion, ok := upgrade.IsUpgradeMsg(sdkTx.GetMsgs()); ok {
			if !IsSupported(appVersion) {
				panic(fmt.Sprintf("network has upgraded to version %d which is not supported by this node. Please upgrade and restart", appVersion))
			}
			app.UpgradeKeeper.PrepareUpgradeAtEndBlock(appVersion)
			// TODO: we may want to emit an event for this
			return abci.ResponseDeliverTx{Code: abci.CodeTypeOK}
		}
	}
	return app.BaseApp.DeliverTx(req)
}
