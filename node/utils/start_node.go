package utils

import (
	"context"
	"fmt"
	"os"

	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/client/flags"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	tmdb "github.com/tendermint/tm-db"
)

func StartNode(ctx context.Context, config *testnode.Config, multiplexer *Multiplexer) (cctx testnode.Context, err error) {
	tempDir, err := os.MkdirTemp("", "example")
	if err != nil {
		return cctx, fmt.Errorf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	baseDir, err := genesis.InitFiles(tempDir, config.TmConfig, config.Genesis, 0)
	if err != nil {
		return testnode.Context{}, err
	}

	cometNode, app, err := newCometNode(baseDir, &config.UniversalTestingConfig, multiplexer)
	if err != nil {
		return testnode.Context{}, err
	}

	cctx = testnode.NewContext(ctx, config.Genesis.Keyring(), config.TmConfig, config.Genesis.ChainID, config.AppConfig.API.Address)

	cctx, stopNode, err := testnode.StartNode(cometNode, cctx)
	if err != nil {
		return testnode.Context{}, err
	}
	defer stopNode()

	cctx, cleanupGRPC, err := testnode.StartGRPCServer(app, config.AppConfig, cctx)
	if err != nil {
		return testnode.Context{}, err
	}
	defer cleanupGRPC()

	_, err = testnode.StartAPIServer(app, *config.AppConfig, cctx)
	if err != nil {
		return testnode.Context{}, err
	}

	return cctx, nil
}

func newCometNode(baseDir string, config *testnode.UniversalTestingConfig, multiplexer *Multiplexer) (cometNode *node.Node, app servertypes.Application, err error) {
	config.AppOptions.Set(flags.FlagHome, baseDir)

	logger := newLogger()
	// dbPath := filepath.Join(config.TmConfig.RootDir, "data")
	dbPath := config.TmConfig.DBPath
	db, err := tmdb.NewGoLevelDB("application", dbPath)
	if err != nil {
		return nil, nil, err
	}
	app = config.AppCreator(logger, db, nil, config.AppOptions)
	nodeKey, err := p2p.LoadOrGenNodeKey(config.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}
	cometNode, err = node.NewNode(
		config.TmConfig,
		privval.LoadOrGenFilePV(config.TmConfig.PrivValidatorKeyFile(), config.TmConfig.PrivValidatorStateFile()),
		nodeKey,
		// newProxyClientCreator(multiplexer),
		proxy.NewLocalClientCreator(app),
		node.DefaultGenesisDocProviderFunc(config.TmConfig),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(config.TmConfig.Instrumentation),
		newLogger(),
	)
	if err != nil {
		return nil, nil, err
	}
	return cometNode, app, err
}

func newProxyClientCreator(multiplexer *Multiplexer) proxy.ClientCreator {
	return proxy.NewLocalClientCreator(multiplexer)
}

func newLogger() log.Logger {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	logger = log.NewFilter(logger, log.AllowDebug())
	return logger
}
