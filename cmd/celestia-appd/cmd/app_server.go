package cmd

import (
	"io"
	"time"

	"cosmossdk.io/log"
	storecache "cosmossdk.io/store/cache"
	"github.com/celestiaorg/celestia-app/v11/app"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

// disableOptimisticExecutionKey turns optimistic execution off on this node (top-level app.toml key or env var).
const disableOptimisticExecutionKey = "disable-optimistic-execution"

// interBlockCacheSize is entries per store, up from the SDK's 1,000: 4x the default 5,000-tx
// mempool, so the recheck working set and each block's fibre key writes stay cached.
const interBlockCacheSize = 20_000

func NewAppServer(logger log.Logger, db dbm.DB, traceStore io.Writer, appOptions servertypes.AppOptions) servertypes.Application {
	var delayedPrecommitTimeout time.Duration
	if v := appOptions.Get(DelayedPrecommitTimeoutFlag); v != nil {
		delayedPrecommitTimeout = cast.ToDuration(v)
	}

	var timeoutCommit time.Duration
	if v := appOptions.Get(TimeoutCommitFlag); v != nil {
		timeoutCommit = cast.ToDuration(v)
	}

	baseAppOptions := server.DefaultBaseappOptions(appOptions)
	if cast.ToBool(appOptions.Get(server.FlagInterBlockCache)) {
		baseAppOptions = append(baseAppOptions, baseapp.SetInterBlockCache(storecache.NewCommitKVStoreCacheManager(interBlockCacheSize)))
	}
	disableOE := cast.ToBool(appOptions.Get(disableOptimisticExecutionKey))
	if disableOE {
		baseAppOptions = append(baseAppOptions, baseapp.DisableOptimisticExecution())
	}
	logger.Info("optimistic execution", "enabled", !disableOE)

	celestiaApp := app.New(
		logger,
		db,
		traceStore,
		delayedPrecommitTimeout,
		timeoutCommit,
		appOptions,
		baseAppOptions...,
	)
	if v := appOptions.Get(DynamicTimeoutCommitFlag); v != nil {
		celestiaApp.SetDynamicTimeoutCommit(cast.ToDuration(v))
	}
	return celestiaApp
}
