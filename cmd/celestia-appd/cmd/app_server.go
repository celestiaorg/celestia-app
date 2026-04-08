package cmd

import (
	"io"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v9/app"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

func NewAppServer(logger log.Logger, db dbm.DB, traceStore io.Writer, appOptions servertypes.AppOptions) servertypes.Application {
	var delayedPrecommitTimeout time.Duration
	if v := appOptions.Get(DelayedPrecommitTimeoutFlag); v != nil {
		delayedPrecommitTimeout = cast.ToDuration(v)
	}

	var timeoutCommit time.Duration
	if v := appOptions.Get(TimeoutCommitFlag); v != nil {
		timeoutCommit = cast.ToDuration(v)
	}

	return app.New(
		logger,
		db,
		traceStore,
		delayedPrecommitTimeout,
		timeoutCommit,
		appOptions,
		server.DefaultBaseappOptions(appOptions)...,
	)
}
