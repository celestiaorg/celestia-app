package cmd

import (
	"io"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/snapshots"
	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cast"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"
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

	// Try to determine the app version or use latest
	appVersion := getAppVersion(logger, db)

	// Create the appropriate encoding configuration based on the app version
	var encodingConfig encoding.Config
	if appVersion < v3.Version {
		// For versions 1 and 2, use the standard encoding config without recursion limits
		logger.Info("Using standard encoding config without recursion limits", "app_version", appVersion)
		encodingConfig = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	} else {
		// For version 3+, use the versioned encoding config with recursion limits
		logger.Info("Using versioned encoding config with recursion limits", "app_version", appVersion)
		encodingConfig = encoding.MakeVersionedConfig(appVersion, app.ModuleEncodingRegisters...)
	}

	return app.New(
		logger,
		db,
		traceStore,
		cast.ToUint(appOptions.Get(server.FlagInvCheckPeriod)),
		encodingConfig,
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

// getAppVersion attempts to get the app version from the consensus params store
// or returns the latest version if not available
func getAppVersion(logger log.Logger, db dbm.DB) uint64 {
	// Get app version from consensus params or use latest version if not available
	var appVersion uint64

	// Try to get the app version from the database directly using the param store key
	// This avoids the need to create a full multi-store
	paramStore := dbm.NewPrefixDB(db, []byte("params"))
	consensusParamsKey := []byte("consensus_params")

	bz, err := paramStore.Get(consensusParamsKey)
	if err == nil && bz != nil {
		var params tmproto.ConsensusParams
		if err := params.Unmarshal(bz); err == nil {
			// Check if AppVersion is set (greater than 0)
			if params.Version.AppVersion > 0 {
				appVersion = params.Version.AppVersion
				logger.Info("Using app version from consensus params", "app_version", appVersion)
			}
		}
	}

	// If we couldn't get the app version from consensus params, use the latest version
	if appVersion == 0 {
		appVersion = appconsts.LatestVersion
		logger.Info("Using latest app version", "app_version", appVersion)
	}

	return appVersion
}
