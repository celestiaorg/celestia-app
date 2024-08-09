package utils

import (
	"context"

	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	tmdb "github.com/tendermint/tm-db"
)

func StartNode(ctx context.Context, config *testnode.Config, multiplexer *Multiplexer, rootDir string) (cctx testnode.Context, cleanup func() error, err error) {
	basePath, err := genesis.InitFiles(config.TmConfig.RootDir, config.TmConfig, config.Genesis, 0)
	if err != nil {
		return testnode.Context{}, nil, err
	}
	config.AppOptions.Set(flags.FlagHome, basePath)

	cometNode, cleanupComet, err := newCometNode(config, multiplexer)
	if err != nil {
		return testnode.Context{}, nil, err
	}

	cctx = testnode.NewContext(ctx, config.Genesis.Keyring(), config.TmConfig, config.Genesis.ChainID, config.AppConfig.API.Address)

	cctx, cleanupNode, err := testnode.StartNode(cometNode, cctx)
	if err != nil {
		return testnode.Context{}, nil, err
	}

	cleanup = func() error {
		cleanupComet()
		cleanupNode()
		return nil
	}

	return cctx, cleanup, nil
}

// HACKHACK: this is a temporary solution to get the CometBFT node running. The
// CometBFT node is connected to the multiplexer but the returned application is
// a singular app (not a multiplexed app).
func newCometNode(config *testnode.Config, multiplexer *Multiplexer) (cometNode *node.Node, cleanupComet func() error, err error) {
	logger := testnode.NewLogger(&config.UniversalTestingConfig)
	db, err := tmdb.NewGoLevelDB("application", config.TmConfig.DBDir())
	if err != nil {
		return nil, nil, err
	}

	// TODO: remove this line
	// app = config.AppCreator(logger, db, nil, config.AppOptions)
	nodeKey, err := p2p.LoadOrGenNodeKey(config.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}
	cometNode, err = node.NewNode(
		config.TmConfig,
		privval.LoadOrGenFilePV(config.TmConfig.PrivValidatorKeyFile(), config.TmConfig.PrivValidatorStateFile()),
		nodeKey,
		// TODO: use multiplexer instead of singular app
		newProxyClientCreator(multiplexer),
		// proxy.NewLocalClientCreator(app),
		node.DefaultGenesisDocProviderFunc(config.TmConfig),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(config.TmConfig.Instrumentation),
		logger,
	)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() error {
		return db.Close()
	}
	return cometNode, cleanup, err
}

func newProxyClientCreator(multiplexer *Multiplexer) proxy.ClientCreator {
	return proxy.NewLocalClientCreator(multiplexer)
}
