package cmd

import (
	"io"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
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
	// The export command sets server.KeyReadOnlyLoad, so these options keep
	// the load from repairing stores, using deferred persistence or writing
	// the IAVL fast-node index.
	application := app.New(logger, db, traceStore, uint(1), config, 0, 0, appOptions, app.PerformanceBaseappOptions(appOptions)...)
	if height != -1 {
		if err := application.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	}
	return application.ExportAppStateAndValidators(forZeroHeight, jailWhiteList)
}
