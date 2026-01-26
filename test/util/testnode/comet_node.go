package testnode

import (
	"path/filepath"

	tmconfig "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdkserver "github.com/cosmos/cosmos-sdk/server"
	servercmtlog "github.com/cosmos/cosmos-sdk/server/log"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
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
	config.AppOptions.Set(TimeoutCommitFlag, config.TmConfig.Consensus.TimeoutCommit)
	app := config.AppCreator(logger, db, nil, config.AppOptions)

	nodeKey, err := p2p.LoadOrGenNodeKey(config.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}

	prival := privval.LoadOrGenFilePV(config.TmConfig.PrivValidatorKeyFile(), config.TmConfig.PrivValidatorStateFile())

	cmtApp := sdkserver.NewCometABCIWrapper(app)
	cometNode, err := node.NewNode(
		config.TmConfig,
		prival,
		nodeKey,
		proxy.NewLocalClientCreator(cmtApp),
		getGenDocProvider(config.TmConfig),
		tmconfig.DefaultDBProvider,
		node.DefaultMetricsProvider(config.TmConfig.Instrumentation),
		servercmtlog.CometLoggerWrapper{Logger: logger},
	)

	return cometNode, app, err
}

// getGenDocProvider returns a function that loads the genesis document from file.
// This uses the SDK's AppGenesis format and converts it to CometBFT's GenesisDoc,
// which properly handles the type conversion (e.g., InitialHeight as int64 vs string).
func getGenDocProvider(cfg *tmconfig.Config) func() (*cmttypes.GenesisDoc, error) {
	return func() (*cmttypes.GenesisDoc, error) {
		appGenesis, err := genutiltypes.AppGenesisFromFile(cfg.GenesisFile())
		if err != nil {
			return nil, err
		}
		return appGenesis.ToGenesisDoc()
	}
}
