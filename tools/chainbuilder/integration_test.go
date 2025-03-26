package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"cosmossdk.io/log"
	cmtcfg "github.com/cometbft/cometbft/config"
	tmlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/rpc/client/local"
	tmdbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
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

	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.SetRoot(cfg.ExistingDir)

	appDB, err := tmdbm.NewDB("application", tmdbm.GoLevelDBBackend, tmCfg.DBDir())
	require.NoError(t, err)

	app := app.New(
		log.NewNopLogger(),
		appDB,
		nil,
		0, // timeout commit
		util.EmptyAppOptions{},
		baseapp.SetMinGasPrices(fmt.Sprintf("%f%s", appconsts.DefaultMinGasPrice, appconsts.BondDenom)),
	)

	nodeKey, err := p2p.LoadNodeKey(tmCfg.NodeKeyFile())
	require.NoError(t, err)

	cmtApp := server.NewCometABCIWrapper(app)
	cometNode, err := node.NewNode(
		tmCfg,
		privval.LoadOrGenFilePV(tmCfg.PrivValidatorKeyFile(), tmCfg.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(cmtApp),
		node.DefaultGenesisDocProviderFunc(tmCfg),
		cmtcfg.DefaultDBProvider,
		node.DefaultMetricsProvider(tmCfg.Instrumentation),
		tmlog.NewNopLogger(),
	)
	require.NoError(t, err)

	require.NoError(t, cometNode.Start())
	defer func() { _ = cometNode.Stop() }()

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
	require.NoError(t, cometNode.Stop())
	cometNode.Wait()
}
