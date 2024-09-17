package testnode

import (
	"path/filepath"

	"github.com/cosmos/cosmos-sdk/client/flags"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	tmdb "github.com/tendermint/tm-db"
)

// NewCometNode creates a ready to use comet node that operates a single
// validator celestia-app network. It expects that all configuration files are
// already initialized and saved to the baseDir.
func NewCometNode(baseDir string, config *UniversalTestingConfig) (*node.Node, servertypes.Application, error) {
	logger := NewLogger(config)
	dbPath := filepath.Join(config.TmConfig.RootDir, "data")
	db, err := tmdb.NewGoLevelDB("application", dbPath)
	if err != nil {
		return nil, nil, err
	}

	config.AppOptions.Set(flags.FlagHome, baseDir)

	app := config.AppCreator(logger, db, nil, config.AppOptions)

	nodeKey, err := p2p.LoadOrGenNodeKey(config.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}

	cometNode, err := node.NewNode(
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
