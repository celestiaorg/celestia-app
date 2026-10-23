package cmd

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log"
	"cosmossdk.io/store/iavl"
	pruningtypes "cosmossdk.io/store/pruning/types"
	"cosmossdk.io/store/rootmulti"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v11/test/util/testnode"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	cometbftdb "github.com/cometbft/cometbft-db"
	cmtcmd "github.com/cometbft/cometbft/cmd/cometbft/commands"
	"github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	gogotypes "github.com/cosmos/gogoproto/types"
	"github.com/stretchr/testify/require"
)

func TestRollbackSameCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rollback integration test in short mode")
	}
	t.Parallel()
	cfg := newRollbackHome(t)
	before := runNode(t, cfg, NewAppServer, 8)
	s := heights(t, cfg)[1]

	require.NoError(t, runRollback(t, cfg, "--height", fmt.Sprint(s-1)))
	require.Equal(t, [3]int64{s, s - 1, s - 1}, heights(t, cfg))

	after := runNode(t, cfg, NewAppServer, s+2)
	require.Equal(t, before[s], after[s])
}

func TestRollbackChangedCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rollback integration test in short mode")
	}
	t.Parallel()
	cfg := newRollbackHome(t)
	before := runNode(t, cfg, NewAppServer, 8)
	s := heights(t, cfg)[1]
	require.NotEmpty(t, before[s])

	require.NoError(t, runRollback(t, cfg, "--height", fmt.Sprint(s-1)))
	after := runNode(t, cfg, mintSkipAt(s), s+2)
	require.NotEqual(t, before[s], after[s])
}

func TestRollbackResumeAfterCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rollback integration test in short mode")
	}
	t.Parallel()
	t.Run("core only", func(t *testing.T) {
		t.Parallel()
		cfg := newRollbackHome(t)
		runNode(t, cfg, NewAppServer, 8)
		s := heights(t, cfg)[1]
		_, _, err := cmtcmd.RollbackStateToHeight(cfg.TmConfig, s-1, false)
		require.NoError(t, err)

		require.ErrorIs(t, runRollback(t, cfg), state.ErrBlockNotExecuted)
		require.NoError(t, runRollback(t, cfg, "--height", fmt.Sprint(s-1)))
		require.Equal(t, [3]int64{s, s - 1, s - 1}, heights(t, cfg))
		runNode(t, cfg, NewAppServer, s+2)
	})

	t.Run("app half rolled back", func(t *testing.T) {
		t.Parallel()
		cfg := newRollbackHome(t)
		runNode(t, cfg, NewAppServer, 8)
		s := heights(t, cfg)[1]
		target := s - 1
		_, _, err := cmtcmd.RollbackStateToHeight(cfg.TmConfig, target, false)
		require.NoError(t, err)

		// Leave the app DB as an app rollback killed after cutting the bank store.
		db := openAppDB(t, cfg)
		capp := newPlainApp(t, db)
		rs := capp.CommitMultiStore().(*rootmulti.Store)
		require.NoError(t, rs.GetCommitKVStore(capp.GetKey(banktypes.StoreKey)).(*iavl.Store).LoadVersionForOverwriting(target))
		bz, err := gogotypes.StdInt64Marshal(target)
		require.NoError(t, err)
		require.NoError(t, db.SetSync([]byte("s/rollback"), bz)) // rootmulti's marker for an unfinished rollback
		require.NoError(t, capp.Close())

		requirePendingRollbackPanic(t, cfg, target)
		require.NoError(t, runRollback(t, cfg, "--height", fmt.Sprint(target)))
		require.Equal(t, [3]int64{s, target, target}, heights(t, cfg))
		runNode(t, cfg, NewAppServer, s+2)
	})
}

func TestRollbackPrunedState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rollback integration test in short mode")
	}
	t.Parallel()
	t.Run("app versions", func(t *testing.T) {
		t.Parallel()
		cfg := newRollbackHome(t)
		cfg.AppOptions.Set(server.FlagPruning, pruningtypes.PruningOptionCustom)
		cfg.AppOptions.Set(server.FlagPruningKeepRecent, uint64(2))
		cfg.AppOptions.Set(server.FlagPruningInterval, uint64(10))
		runNode(t, cfg, NewAppServer, 12)
		before := heights(t, cfg)

		require.ErrorContains(t, runRollback(t, cfg, "--height", "5"), "store acc has no version 5")
		require.Equal(t, before, heights(t, cfg))
	})

	t.Run("blocks", func(t *testing.T) {
		t.Parallel()
		cfg := newRollbackHome(t)
		cfg.AppOptions.Set(server.FlagMinRetainBlocks, uint64(3))
		// Snapshots off, so the snapshot window does not hold back block pruning.
		cfg.AppOptions.Set(server.FlagStateSyncSnapshotInterval, uint64(0))
		cfg.AppOptions.Set(server.FlagStateSyncSnapshotKeepRecent, uint32(0))
		runNode(t, cfg, NewAppServer, 10)
		before := heights(t, cfg)

		require.ErrorContains(t, runRollback(t, cfg, "--height", "2"), "out of range")
		require.Equal(t, before, heights(t, cfg))
	})
}

func TestRollbackRefusesForeignAppVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rollback integration test in short mode")
	}
	t.Parallel()
	cfg := newRollbackHome(t)
	runNode(t, cfg, NewAppServer, 3)
	saveStateAppVersion(t, cfg.TmConfig, appconsts.Version-1)
	before := heights(t, cfg)

	require.ErrorContains(t, runRollback(t, cfg), fmt.Sprintf("passthrough v%d rollback", appconsts.Version-1))
	require.Equal(t, before, heights(t, cfg))
}

// newRollbackHome writes the config and genesis files of a single validator node.
func newRollbackHome(t *testing.T) *testnode.Config {
	t.Helper()
	// A long timeout commit lets runNode stop the node before it signs the next height.
	cfg := testnode.DefaultConfig().WithTimeoutCommit(time.Second)
	// Node.OnStop leaves the tx indexer DB open, which blocks a restart in the same process.
	cfg.TmConfig.TxIndex.Indexer = "null"
	require.NoError(t, genesis.InitFiles(t.TempDir(), cfg.TmConfig, cfg.AppConfig, cfg.Genesis, 0))
	return cfg
}

// runNode runs the node until block height until and returns the app hash after each executed height.
func runNode(t *testing.T, cfg *testnode.Config, creator servertypes.AppCreator, until int64) map[int64][]byte {
	t.Helper()
	// Fresh ports per start: while the node is stopped, a test in another package can take the old ones.
	tm := cfg.TmConfig
	tm.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetDeterministicPort())
	tm.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetDeterministicPort())
	tm.RPC.GRPCListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetDeterministicPort())
	tm.PrivValidatorGRPCListenAddr = fmt.Sprintf("127.0.0.1:%d", testnode.GetDeterministicPort())

	utc := cfg.UniversalTestingConfig
	utc.AppCreator = creator
	node, capp, err := testnode.NewCometNode(cfg.TmConfig.RootDir, &utc)
	require.NoError(t, err)
	require.NoError(t, node.Start())
	require.Eventually(t, func() bool { return node.BlockStore().Height() >= until }, time.Minute, 50*time.Millisecond)

	appHash := make(map[int64][]byte)
	bs := node.BlockStore()
	for h := bs.Base() + 1; h <= bs.Height(); h++ {
		if meta := bs.LoadBlockMeta(h); meta != nil {
			appHash[h-1] = meta.Header.AppHash
		}
	}
	require.NoError(t, node.Stop())
	node.Wait()
	id := capp.CommitMultiStore().LastCommitID()
	appHash[id.Version] = id.Hash
	require.NoError(t, capp.Close())
	return appHash
}

// runRollback runs the rollback command of celestia-appd on the node home.
func runRollback(t *testing.T, cfg *testnode.Config, args ...string) error {
	t.Helper()
	home := cfg.TmConfig.RootDir
	root := NewRootCmd()
	root.SetArgs(append([]string{"rollback", "--home", home}, args...))
	return svrcmd.Execute(root, app.EnvPrefix, home)
}

// heights returns the block store, CometBFT state and app heights.
func heights(t *testing.T, cfg *testnode.Config) [3]int64 {
	t.Helper()
	backend := cometbftdb.BackendType(cfg.TmConfig.DBBackend)
	blockDB, err := cometbftdb.NewDB("blockstore", backend, cfg.TmConfig.BlockstoreDir())
	require.NoError(t, err)
	blockStore := store.NewBlockStore(blockDB)
	defer func() { require.NoError(t, blockStore.Close()) }()

	stateDB, err := cometbftdb.NewDB("state", backend, cfg.TmConfig.DBDir())
	require.NoError(t, err)
	stateStore := state.NewStore(stateDB, state.StoreOptions{})
	defer func() { require.NoError(t, stateStore.Close()) }()
	st, err := stateStore.Load()
	require.NoError(t, err)

	appDB := openAppDB(t, cfg)
	defer func() { require.NoError(t, appDB.Close()) }()

	return [3]int64{blockStore.Height(), st.LastBlockHeight, rootmulti.GetLatestVersion(appDB)}
}

func openAppDB(t *testing.T, cfg *testnode.Config) dbm.DB {
	t.Helper()
	db, err := dbm.NewDB("application", dbm.BackendType(cfg.TmConfig.DBBackend), cfg.TmConfig.DBDir())
	require.NoError(t, err)
	return db
}

// newPlainApp opens the app without baseapp options, so it opens no other DBs.
func newPlainApp(t *testing.T, db dbm.DB) *app.App {
	return app.New(log.NewNopLogger(), db, nil, 0, 0, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()))
}

// requirePendingRollbackPanic requires a normal app open to refuse the unfinished rollback to target.
func requirePendingRollbackPanic(t *testing.T, cfg *testnode.Config, target int64) {
	t.Helper()
	db := openAppDB(t, cfg)
	defer func() {
		require.NoError(t, db.Close())
		err, ok := recover().(error)
		require.True(t, ok)
		require.ErrorIs(t, err, rootmulti.ErrRollbackInProgress{Target: target})
	}()
	newPlainApp(t, db)
}

// mintSkipAt returns NewAppServer with the mint BeginBlock skipped at height.
func mintSkipAt(height int64) servertypes.AppCreator {
	return func(logger log.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
		capp := NewAppServer(logger, db, traceStore, appOpts).(*app.App)
		capp.ModuleManager.Modules[minttypes.ModuleName] = skipBeginBlock{
			HasBeginBlocker: capp.ModuleManager.Modules[minttypes.ModuleName].(appmodule.HasBeginBlocker),
			height:          height,
		}
		return capp
	}
}

type skipBeginBlock struct {
	appmodule.HasBeginBlocker
	height int64
}

func (m skipBeginBlock) BeginBlock(ctx context.Context) error {
	if sdk.UnwrapSDKContext(ctx).BlockHeight() == m.height {
		return nil
	}
	return m.HasBeginBlocker.BeginBlock(ctx)
}
