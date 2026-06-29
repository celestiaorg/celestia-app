package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/test/util"
	"github.com/celestiaorg/celestia-app/v9/test/util/random"
	"github.com/celestiaorg/celestia-app/v9/test/util/testnode"
	dbm "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	tmlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/rpc/client/local"
	cmttypes "github.com/cometbft/cometbft/types"
	tmdbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chainbuilder tool test")
	}

	numBlocks := 10

	cfg := BuilderConfig{
		NumBlocks:     numBlocks,
		BlockSize:     appconsts.DefaultMaxBytes,
		BlockInterval: time.Second,
		ChainID:       random.Str(6),
		Namespace:     defaultNamespace,
	}

	dir := t.TempDir()

	// First run
	err := Run(context.Background(), cfg, dir)
	require.NoError(t, err)

	// Second run with existing directory
	cfg.ExistingDir = filepath.Join(dir, fmt.Sprintf("testnode-%s", cfg.ChainID))
	err = Run(context.Background(), cfg, dir)
	require.NoError(t, err)

	// retry node start with fresh ports on binding errors (TOCTOU in
	// GetDeterministicPort can cause collisions under -race).
	const maxRetries = 3
	var cometNode *node.Node
	for attempt := range maxRetries {
		tmCfg := testnode.DefaultTendermintConfig()
		tmCfg.SetRoot(cfg.ExistingDir)

		appDB, err := tmdbm.NewDB("application", tmdbm.BackendType(tmCfg.DBBackend), tmCfg.DBDir())
		require.NoError(t, err)

		celestiaApp := app.New(
			log.NewNopLogger(),
			appDB,
			nil,
			0, // delayed precommit timeout
			0, // timeout commit
			util.EmptyAppOptions{},
			baseapp.SetMinGasPrices(fmt.Sprintf("%f%s", appconsts.DefaultMinGasPrice, appconsts.BondDenom)),
		)

		nodeKey, err := p2p.LoadNodeKey(tmCfg.NodeKeyFile())
		require.NoError(t, err)

		// Track the databases CometBFT opens (blockstore, state, etc.) so they
		// can be closed if this attempt fails. Otherwise their file locks leak
		// and the retry fails with "lock held by current process".
		var nodeDBs []dbm.DB
		dbProvider := func(ctx *cmtcfg.DBContext) (dbm.DB, error) {
			db, err := cmtcfg.DefaultDBProvider(ctx)
			if err != nil {
				return nil, err
			}
			nodeDBs = append(nodeDBs, db)
			return db, nil
		}

		cmtApp := server.NewCometABCIWrapper(celestiaApp)
		cometNode, err = node.NewNode(
			tmCfg,
			privval.LoadOrGenFilePV(tmCfg.PrivValidatorKeyFile(), tmCfg.PrivValidatorStateFile()),
			nodeKey,
			proxy.NewLocalClientCreator(cmtApp),
			getGenDocProvider(tmCfg),
			dbProvider,
			node.DefaultMetricsProvider(tmCfg.Instrumentation),
			tmlog.NewNopLogger(),
		)
		if err == nil {
			err = cometNode.Start()
		}
		if err != nil {
			_ = appDB.Close()
			for _, db := range nodeDBs {
				_ = db.Close()
			}
			if testnode.IsPortBindingError(err) && attempt < maxRetries-1 {
				t.Logf("port binding error on attempt %d/%d, retrying: %v", attempt+1, maxRetries, err)
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			require.NoError(t, err)
		}
		break
	}
	defer func() {
		_ = cometNode.Stop()
		cometNode.Wait()
	}()

	client := local.New(cometNode)
	status, err := client.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	// assert that the new node eventually makes progress in the chain
	require.Eventually(t, func() bool {
		status, err := client.Status(context.Background())
		require.NoError(t, err)
		return status.SyncInfo.LatestBlockHeight >= int64(numBlocks*2)
	}, time.Second*10, time.Millisecond*100)
}

// getGenDocProvider returns a function that loads the genesis document from file.
// This uses the SDK's AppGenesis format and converts it to CometBFT's GenesisDoc,
// which properly handles the type conversion (e.g., InitialHeight as int64 vs string).
func getGenDocProvider(cfg *cmtcfg.Config) func() (*cmttypes.GenesisDoc, error) {
	return func() (*cmttypes.GenesisDoc, error) {
		appGenesis, err := genutiltypes.AppGenesisFromFile(cfg.GenesisFile())
		if err != nil {
			return nil, err
		}
		return appGenesis.ToGenesisDoc()
	}
}
