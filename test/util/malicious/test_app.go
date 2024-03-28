package malicious

import (
	"io"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
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

// OutOfOrderNamesapceConfig returns a testnode config that will start producing
// blocks with out of order namespaces at the provided height.
//
// Note: per the OutOfOrder go docs, the first two blobs with different
// namespaces will be swapped, resulting in an invalid block.
func OutOfOrderNamespaceConfig(startHeight int64) *testnode.Config {
	bcfg := BehaviorConfig{StartHeight: startHeight, HandlerName: OutOfOrderHandlerKey}
	return TestNodeConfig(bcfg)
}

// TestNodeConfig returns a testnode config with the malicious application and
// provided behavior set in the app options.
func TestNodeConfig(behavior BehaviorConfig) *testnode.Config {
	cfg := testnode.DefaultConfig().
		WithAppCreator(NewAppServer)

	cfg.AppOptions.Set(BehaviorConfigKey, behavior)
	return cfg
}

// NewTestApp creates a new malicious application with the provided consensus
// params.
func NewTestApp(cparams *tmproto.ConsensusParams, mcfg BehaviorConfig, genAccounts ...string) *App {
	app, _ := util.SetupTestAppWithGenesisValSet(cparams, genAccounts...)
	badapp := &App{App: app}
	badapp.SetMaliciousBehavior(mcfg)
	return badapp
}

// NewAppServer creates a new AppServer using the malicious application.
func NewAppServer(logger log.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
	var cache sdk.MultiStorePersistentCache

	if cast.ToBool(appOpts.Get(server.FlagInterBlockCache)) {
		cache = store.NewCommitKVStoreCacheManager()
	}

	pruningOpts, err := server.GetPruningOptionsFromFlags(appOpts)
	if err != nil {
		panic(err)
	}

	// Add snapshots
	snapshotDir := filepath.Join(cast.ToString(appOpts.Get(flags.FlagHome)), "data", "snapshots")
	//nolint: staticcheck
	snapshotDB, err := sdk.NewLevelDB("metadata", snapshotDir)
	if err != nil {
		panic(err)
	}
	snapshotStore, err := snapshots.NewStore(snapshotDB, snapshotDir)
	if err != nil {
		panic(err)
	}

	return New(
		logger, db, traceStore, true,
		cast.ToUint(appOpts.Get(server.FlagInvCheckPeriod)),
		encoding.MakeConfig(app.ModuleEncodingRegisters...), // Ideally, we would reuse the one created by NewRootCmd.
		appOpts,
		baseapp.SetPruning(pruningOpts),
		baseapp.SetMinGasPrices(cast.ToString(appOpts.Get(server.FlagMinGasPrices))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOpts.Get(server.FlagMinRetainBlocks))),
		baseapp.SetHaltHeight(cast.ToUint64(appOpts.Get(server.FlagHaltHeight))),
		baseapp.SetHaltTime(cast.ToUint64(appOpts.Get(server.FlagHaltTime))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOpts.Get(server.FlagMinRetainBlocks))),
		baseapp.SetInterBlockCache(cache),
		baseapp.SetTrace(cast.ToBool(appOpts.Get(server.FlagTrace))),
		baseapp.SetIndexEvents(cast.ToStringSlice(appOpts.Get(server.FlagIndexEvents))),
		baseapp.SetSnapshot(snapshotStore, snapshottypes.NewSnapshotOptions(cast.ToUint64(appOpts.Get(server.FlagStateSyncSnapshotInterval)), cast.ToUint32(appOpts.Get(server.FlagStateSyncSnapshotKeepRecent)))),
	)
}
