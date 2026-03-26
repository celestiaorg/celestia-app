package cmd

import (
	"io"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v8/app"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

func NewAppServer(logger log.Logger, db dbm.DB, traceStore io.Writer, appOptions servertypes.AppOptions) servertypes.Application {
	// Check for the new --delayed-precommit-timeout flag first, then fall back to deprecated --timeout-commit
	var delayedPrecommitTimeout time.Duration
	if delayedPrecommitTimeoutFromFlag := appOptions.Get(DelayedPrecommitTimeoutFlag); delayedPrecommitTimeoutFromFlag != nil {
		delayedPrecommitTimeout = cast.ToDuration(delayedPrecommitTimeoutFromFlag)
	} else if timeoutCommitFromFlag := appOptions.Get(TimeoutCommitFlag); timeoutCommitFromFlag != nil {
		delayedPrecommitTimeout = cast.ToDuration(timeoutCommitFromFlag)
	}

	return app.New(
		logger,
		db,
		traceStore,
		delayedPrecommitTimeout,
		appOptions,
		server.DefaultBaseappOptions(appOptions)...,
	)
}
