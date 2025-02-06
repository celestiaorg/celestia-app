package cmd

import (
	"io"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/cometbft/cometbft/libs/log"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
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
	application := app.New(logger, db, traceStore, uint(1), config, 0, 0, appOptions)
	if height != -1 {
		if err := application.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	}
	return application.ExportAppStateAndValidators(forZeroHeight, jailWhiteList)
}
