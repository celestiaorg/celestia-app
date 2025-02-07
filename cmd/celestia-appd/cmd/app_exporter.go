package cmd

import (
	"io"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
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
) (servertypes.ExportedApp, error) {
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	application := app.New(logger, db, traceStore, uint(1), config, 0, 0, appOptions)
	if height != -1 {
		if err := application.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	}
	return application.ExportAppStateAndValidators(forZeroHeight, jailWhiteList)
}
