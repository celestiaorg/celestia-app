package cmd

import (
	"io"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/app"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

func NewAppServer(logger log.Logger, db dbm.DB, traceStore io.Writer, appOptions servertypes.AppOptions) servertypes.Application {
	return app.New(
		logger,
		db,
		traceStore,
		cast.ToDuration(appOptions.Get(TimeoutCommitFlag)),
		appOptions,
		server.DefaultBaseappOptions(appOptions)...,
	)
}
