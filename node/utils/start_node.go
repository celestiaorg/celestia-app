package utils

import (
	"context"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/client/flags"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
)

const baseDir = "~/.celestia-app-start-node"

func StartNode(config *testnode.Config, multiplexer *Multiplexer) (cctx testnode.Context, err error) {
	baseDir, err := genesis.InitFiles(baseDir, config.TmConfig, config.Genesis, 0)
	if err != nil {
		return testnode.Context{}, err
	}

	cometNode, _, err := newCometNode(baseDir, &config.UniversalTestingConfig, multiplexer)
	if err != nil {
		return testnode.Context{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cctx = testnode.NewContext(ctx, config.Genesis.Keyring(), config.TmConfig, config.Genesis.ChainID, config.AppConfig.API.Address)

	cctx, stopNode, err := testnode.StartNode(cometNode, cctx)
	if err != nil {
		return testnode.Context{}, err
	}
	defer stopNode()

	return cctx, nil
}

// newCometNode creates a ready to use comet node that operates a single
// validator celestia-app network. It expects that all configuration files are
// already initialized and saved to the baseDir.
func newCometNode(baseDir string, config *testnode.UniversalTestingConfig, multiplexer *Multiplexer) (cometNode *node.Node, app abci.Application, err error) {
	// dbPath := filepath.Join(config.TmConfig.RootDir, "data")
	// db, err := dbm.NewGoLevelDB("application", dbPath)
	// if err != nil {
	// 	return nil, nil, err
	// }

	config.AppOptions.Set(flags.FlagHome, baseDir)

	logger := newLogger(config)
	app = newApp(multiplexer)

	nodeKey, err := p2p.LoadOrGenNodeKey(config.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}

	cometNode, err = node.NewNode(
		config.TmConfig,
		privval.LoadOrGenFilePV(config.TmConfig.PrivValidatorKeyFile(), config.TmConfig.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(app),
		node.DefaultGenesisDocProviderFunc(config.TmConfig),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(config.TmConfig.Instrumentation),
		logger,
	)

	return cometNode, app, err
}

func newApp(multiplexer *Multiplexer) abci.Application {
	// TODO: need to be able to switch between apps
	return multiplexer.apps[0]
}

func newLogger(config *testnode.UniversalTestingConfig) log.Logger {
	if config.SuppressLogs {
		return log.NewNopLogger()
	}
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	logger = log.NewFilter(logger, log.AllowError())
	return logger
}
