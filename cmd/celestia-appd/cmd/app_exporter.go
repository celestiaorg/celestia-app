package cmd

import (
	"io"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
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
	application := app.NewForExport(logger, db, traceStore, 0, 0, appOptions)
	var err error
	if height == -1 {
		err = application.LoadLatestVersion()
	} else {
		err = application.LoadHeight(height)
	}
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	return application.ExportAppStateAndValidators(forZeroHeight, jailWhiteList, modulesToExport)
}
