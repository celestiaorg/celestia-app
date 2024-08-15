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

func StartNode(ctx context.Context, config *testnode.Config, multiplexer *Multiplexer) (cctx testnode.Context, cleanup func() error, err error) {
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

func newCometNode(config *testnode.Config, multiplexer *Multiplexer) (cometNode *node.Node, cleanupComet func() error, err error) {
	logger := testnode.NewLogger(&config.UniversalTestingConfig)
	db, err := tmdb.NewGoLevelDB("application", config.TmConfig.DBDir())
	if err != nil {
		return nil, nil, err
	}

	nodeKey, err := p2p.LoadOrGenNodeKey(config.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}
	cometNode, err = node.NewNode(
		config.TmConfig,
		privval.LoadOrGenFilePV(config.TmConfig.PrivValidatorKeyFile(), config.TmConfig.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(multiplexer),
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
