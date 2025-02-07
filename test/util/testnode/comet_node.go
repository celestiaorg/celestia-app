package testnode

import (
	"path/filepath"

	tmconfig "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	servercmtlog "github.com/cosmos/cosmos-sdk/server/log"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

// NewCometNode creates a ready to use comet node that operates a single
// validator celestia-app network. It expects that all configuration files are
// already initialized and saved to the baseDir.
func NewCometNode(baseDir string, config *UniversalTestingConfig) (*node.Node, servertypes.Application, error) {
	logger := NewLogger(config)
	dbPath := filepath.Join(config.TmConfig.RootDir, "data")

	db, err := dbm.NewGoLevelDB("application", dbPath, dbm.OptionsMap{})
	if err != nil {
		return nil, nil, err
	}

	config.AppOptions.Set(flags.FlagHome, baseDir)
	app := config.AppCreator(logger, db, nil, config.AppOptions)

	nodeKey, err := p2p.LoadOrGenNodeKey(config.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}

	prival := privval.LoadOrGenFilePV(config.TmConfig.PrivValidatorKeyFile(), config.TmConfig.PrivValidatorStateFile())
	if err != nil {
		return nil, nil, err
	}
	cmtApp := sdkserver.NewCometABCIWrapper(app)
	cometNode, err := node.NewNode(
		config.TmConfig,
		prival,
		nodeKey,
		proxy.NewLocalClientCreator(cmtApp),
		node.DefaultGenesisDocProviderFunc(config.TmConfig),
		tmconfig.DefaultDBProvider,
		node.DefaultMetricsProvider(config.TmConfig.Instrumentation),
		servercmtlog.CometLoggerWrapper{Logger: logger},
	)

	return cometNode, app, err
}
