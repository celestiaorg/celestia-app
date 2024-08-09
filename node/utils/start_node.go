package utils

import (
	"context"

	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/client/flags"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
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

	cometNode, app, cleanupComet, err := newCometNode(config, multiplexer)
	if err != nil {
		return testnode.Context{}, nil, err
	}

	cctx = testnode.NewContext(ctx, config.Genesis.Keyring(), config.TmConfig, config.Genesis.ChainID, config.AppConfig.API.Address)

	cctx, cleanupNode, err := testnode.StartNode(cometNode, cctx)
	if err != nil {
		return testnode.Context{}, nil, err
	}

	cctx, cleanupGRPC, err := testnode.StartGRPCServer(app, config.AppConfig, cctx)
	if err != nil {
		return testnode.Context{}, nil, err
	}

	cleanup = func() error {
		cleanupComet()
		cleanupNode()
		cleanupGRPC()
		return nil
	}

	return cctx, cleanup, nil
}

func newCometNode(config *testnode.Config, multiplexer *Multiplexer) (cometNode *node.Node, app servertypes.Application, cleanupComet func() error, err error) {
	logger := testnode.NewLogger(&config.UniversalTestingConfig)
	db, err := tmdb.NewGoLevelDB("application", config.TmConfig.DBDir())
	if err != nil {
		return nil, nil, nil, err
	}

	// TODO: remove this line
	app = config.AppCreator(logger, db, nil, config.AppOptions)
	nodeKey, err := p2p.LoadOrGenNodeKey(config.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, nil, err
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
		return nil, nil, nil, err
	}
	cleanup := func() error {
		return db.Close()
	}
	return cometNode, app, cleanup, err
}

func newProxyClientCreator(multiplexer *Multiplexer) proxy.ClientCreator {
	return proxy.NewLocalClientCreator(multiplexer)
}
