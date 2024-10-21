package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/tendermint/tendermint/libs/log"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/rpc/client/local"
	tmdbm "github.com/tendermint/tm-db"

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
		ChainID:       tmrand.Str(6),
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

	encCfg := encoding.MakeConfig(app.ModuleBasics)

	app := app.New(
		log.NewNopLogger(),
		appDB,
		nil,
		0,
		encCfg,
		0,
		util.EmptyAppOptions{},
		baseapp.SetMinGasPrices(fmt.Sprintf("%f%s", appconsts.DefaultMinGasPrice, appconsts.BondDenom)),
	)

	nodeKey, err := p2p.LoadNodeKey(tmCfg.NodeKeyFile())
	require.NoError(t, err)

	cometNode, err := node.NewNode(
		tmCfg,
		privval.LoadOrGenFilePV(tmCfg.PrivValidatorKeyFile(), tmCfg.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(app),
		node.DefaultGenesisDocProviderFunc(tmCfg),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(tmCfg.Instrumentation),
		log.NewNopLogger(),
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
