package cmd

import (
	"io"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v5/app"
	dbm "github.com/cosmos/cosmos-db"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

func appExporter(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	height int64,
	forZeroHeight bool,
	jailWhiteList []string,
	appOptions servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	// The inter-block cache would make LoadHeight export the latest state.
	application := app.New(logger, db, traceStore, 0, app.WithoutInterBlockCache(appOptions))
	if height != -1 {
		if err := application.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	}
	return application.ExportAppStateAndValidators(forZeroHeight, jailWhiteList, modulesToExport)
}
