package utils

import (
	"context"
	"os"

	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
)

const baseDir = "~/.celestia-app-start-node"

func StartNode(ctx context.Context, config *testnode.Config, multiplexer *Multiplexer) (cctx testnode.Context, err error) {
	baseDir, err := genesis.InitFiles(baseDir, config.TmConfig, config.Genesis, 0)
	if err != nil {
		return testnode.Context{}, err
	}

	cometNode, err := newCometNode(baseDir, &config.UniversalTestingConfig, multiplexer)
	if err != nil {
		return testnode.Context{}, err
	}

	cctx = testnode.NewContext(ctx, config.Genesis.Keyring(), config.TmConfig, config.Genesis.ChainID, config.AppConfig.API.Address)

	cctx, stopNode, err := testnode.StartNode(cometNode, cctx)
	if err != nil {
		return testnode.Context{}, err
	}
	defer stopNode()

	return cctx, nil
}

func newCometNode(baseDir string, config *testnode.UniversalTestingConfig, multiplexer *Multiplexer) (cometNode *node.Node, err error) {
	config.AppOptions.Set(flags.FlagHome, baseDir)
	nodeKey, err := p2p.LoadOrGenNodeKey(config.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, err
	}
	cometNode, err = node.NewNode(
		config.TmConfig,
		privval.LoadOrGenFilePV(config.TmConfig.PrivValidatorKeyFile(), config.TmConfig.PrivValidatorStateFile()),
		nodeKey,
		newProxyClientCreator(multiplexer),
		node.DefaultGenesisDocProviderFunc(config.TmConfig),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(config.TmConfig.Instrumentation),
		newLogger(config),
	)
	if err != nil {
		return nil, err
	}
	return cometNode, err
}

func newProxyClientCreator(multiplexer *Multiplexer) proxy.ClientCreator {
	// TODO: need to be able to switch between apps
	return proxy.NewLocalClientCreator(multiplexer.apps[0])
}

func newLogger(config *testnode.UniversalTestingConfig) log.Logger {
	if config.SuppressLogs {
		return log.NewNopLogger()
	}
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	logger = log.NewFilter(logger, log.AllowError())
	return logger
}
