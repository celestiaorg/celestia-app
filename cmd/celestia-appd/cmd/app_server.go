package cmd

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
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

	return app.New(
		logger,
		db,
		traceStore,
		cast.ToUint(appOptions.Get(server.FlagInvCheckPeriod)),
		encoding.MakeConfig(app.ModuleEncodingRegisters...),
		getUpgradeHeightV2(appOptions),
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

func getUpgradeHeightV2(appOptions servertypes.AppOptions) int64 {
	upgradeHeight := cast.ToInt64(appOptions.Get(UpgradeHeightFlag))
	if upgradeHeight != 0 {
		fmt.Printf("upgrade height flag non-zero so using it: %d\n", upgradeHeight)
		return upgradeHeight
	}

	fmt.Printf("upgrade height flag zero\n")

	// TODO: this chainID doesn't always appear populated.
	chainID := cast.ToString(appOptions.Get(flags.FlagChainID))
	fmt.Printf("chainID %v\n", chainID)

	switch chainID {
	case appconsts.ArabicaChainID:
		return appconsts.ArabicaUpgradeHeightV2
	case appconsts.MochaChainID:
		return appconsts.MochaUpgradeHeightV2
	case appconsts.MainnetChainID:
		return appconsts.MainnetUpgradeHeightV2
	default:
		// default to the upgrade height provided by the flag
		return upgradeHeight
	}
}
