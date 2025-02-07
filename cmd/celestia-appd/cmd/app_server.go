package cmd

import (
	"io"
	"path/filepath"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/snapshots"
	snapshottypes "cosmossdk.io/store/snapshots/types"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cast"
)

func NewAppServer(logger log.Logger, db dbm.DB, traceStore io.Writer, appOptions servertypes.AppOptions) servertypes.Application {
	var cache sdk.MultiStorePersistentCache

	if cast.ToBool(appOptions.Get(server.FlagInterBlockCache)) {
		cache = store.NewCommitKVStoreCacheManager()
	}

	pruningOpts, err := server.GetPruningOptionsFromFlags(appOptions)
	if err != nil {
		panic(err)
	}

	// Add snapshots
	snapshotDir := filepath.Join(cast.ToString(appOptions.Get(flags.FlagHome)), "data", "snapshots")
	//nolint: staticcheck
	snapshotDB, err := sdk.NewLevelDB("metadata", snapshotDir)
	if err != nil {
		panic(err)
	}
	snapshotStore, err := snapshots.NewStore(snapshotDB, snapshotDir)
	if err != nil {
		panic(err)
	}

	return app.New(
		logger,
		db,
		traceStore,
		cast.ToUint(appOptions.Get(server.FlagInvCheckPeriod)),
		encoding.MakeConfig(app.ModuleEncodingRegisters...),
		cast.ToInt64(appOptions.Get(UpgradeHeightFlag)),
		cast.ToDuration(appOptions.Get(TimeoutCommitFlag)),
		appOptions,
		baseapp.SetPruning(pruningOpts),
		baseapp.SetMinGasPrices(cast.ToString(appOptions.Get(server.FlagMinGasPrices))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOptions.Get(server.FlagMinRetainBlocks))),
		baseapp.SetHaltHeight(cast.ToUint64(appOptions.Get(server.FlagHaltHeight))),
		baseapp.SetHaltTime(cast.ToUint64(appOptions.Get(server.FlagHaltTime))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOptions.Get(server.FlagMinRetainBlocks))),
		baseapp.SetInterBlockCache(cache),
		baseapp.SetTrace(cast.ToBool(appOptions.Get(server.FlagTrace))),
		baseapp.SetIndexEvents(cast.ToStringSlice(appOptions.Get(server.FlagIndexEvents))),
		baseapp.SetSnapshot(snapshotStore, snapshottypes.NewSnapshotOptions(cast.ToUint64(appOptions.Get(server.FlagStateSyncSnapshotInterval)), cast.ToUint32(appOptions.Get(server.FlagStateSyncSnapshotKeepRecent)))),
	)
}
