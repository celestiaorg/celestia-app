package cmd

import (
	"io"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/app"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

func NewAppServer(logger log.Logger, db dbm.DB, traceStore io.Writer, appOptions servertypes.AppOptions) servertypes.Application {
	// Check for the new --block-time flag first, then fall back to deprecated --timeout-commit
	var blockTime time.Duration
	if blockTimeFromFlag := appOptions.Get(BlockTimeFlag); blockTimeFromFlag != nil {
		blockTime = cast.ToDuration(blockTimeFromFlag)
	} else if timeoutCommitFromFlag := appOptions.Get(TimeoutCommitFlag); timeoutCommitFromFlag != nil {
		blockTime = cast.ToDuration(timeoutCommitFromFlag)
	}

	return app.New(
		logger,
		db,
		traceStore,
		blockTime,
		appOptions,
		server.DefaultBaseappOptions(appOptions)...,
	)
}
