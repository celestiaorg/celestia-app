package testnode

import (
	"fmt"
	"io"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/test/util/genesis"
	tmconfig "github.com/cometbft/cometbft/config"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
)

const (
	kibibyte                    = 1024 // bytes
	DefaultValidatorAccountName = "validator"
	DefaultInitialBalance       = genesis.DefaultInitialBalance
	// TimeoutCommit is a flag that can be used to override the timeout_commit.
	// Deprecated: Use DelayedPrecommitTimeout instead.
	TimeoutCommitFlag = "timeout-commit"
	// DelayedPrecommitTimeout is a flag that can be used to override the DelayedPrecommitTimeout.
	DelayedPrecommitTimeout = "delayed-precommit-timeout"
)

type UniversalTestingConfig struct {
	// TmConfig is the Tendermint configuration used for the network.
	TmConfig *tmconfig.Config
	// AppConfig is the application configuration of the test node.
	AppConfig *srvconfig.Config
	// AppOptions are the application options of the test node.
	AppOptions *KVAppOptions
	// AppCreator is used to create the application for the testnode.
	AppCreator srvtypes.AppCreator
	// SuppressLogs in testnode. This should be set to true when running
	// network tests.
	SuppressLogs bool
}

// Config is the configuration of a test node.
type Config struct {
	Genesis *genesis.Genesis
	UniversalTestingConfig
}

func (c *Config) WithGenesis(g *genesis.Genesis) *Config {
	c.Genesis = g
	return c
}

// WithTendermintConfig sets the TmConfig and returns the *Config.
func (c *Config) WithTendermintConfig(conf *tmconfig.Config) *Config {
	c.TmConfig = conf
	return c
}

// WithAppConfig sets the AppConfig and returns the Config.
func (c *Config) WithAppConfig(conf *srvconfig.Config) *Config {
	c.AppConfig = conf
	return c
}

// WithAppOptions sets the AppOptions and returns the Config.
func (c *Config) WithAppOptions(opts *KVAppOptions) *Config {
	c.AppOptions = opts
	return c
}

// WithAppCreator sets the AppCreator and returns the Config.
func (c *Config) WithAppCreator(creator srvtypes.AppCreator) *Config {
	c.AppCreator = creator
	return c
}

// WithSuppressLogs sets the SuppressLogs and returns the Config.
func (c *Config) WithSuppressLogs(sl bool) *Config {
	c.SuppressLogs = sl
	return c
}

// WithTimeoutCommit sets the timeout commit in the cometBFT config and returns
// the Config. For backward compatibility, it also sets the app's block time.
// Deprecated: Use WithDelayedPrecommitTimeout instead.
func (c *Config) WithTimeoutCommit(d time.Duration) *Config {
	c.TmConfig.Consensus.TimeoutCommit = d
	// For backward compatibility, also set the app option so existing tests continue to work
	c.AppOptions.Set(TimeoutCommitFlag, d)
	return c
}

// WithDelayedPrecommitTimeout sets the target block time using DelayedPrecommitTimeout in the app
// options and returns the Config. This affects the DelayedPrecommitTimeout used for consistent
// block timing.
func (c *Config) WithDelayedPrecommitTimeout(d time.Duration) *Config {
	c.AppOptions.Set(DelayedPrecommitTimeout, d)
	return c
}

// WithFundedAccounts sets the genesis accounts and returns the Config.
func (c *Config) WithFundedAccounts(accounts ...string) *Config {
	c.Genesis = c.Genesis.WithKeyringAccounts(
		genesis.NewKeyringAccounts(DefaultInitialBalance+1, accounts...)...,
	)
	return c
}

// WithModifiers sets the genesis options and returns the Config.
func (c *Config) WithModifiers(ops ...genesis.Modifier) *Config {
	c.Genesis = c.Genesis.WithModifiers(ops...)
	return c
}

// WithGenesisTime sets the genesis time and returns the Config.
func (c *Config) WithGenesisTime(t time.Time) *Config {
	c.Genesis = c.Genesis.WithGenesisTime(t)
	return c
}

// WithChainID sets the chain ID and returns the Config.
func (c *Config) WithChainID(id string) *Config {
	c.Genesis = c.Genesis.WithChainID(id)
	return c
}

// WithConsensusParams sets the consensus params and returns the Config.
func (c *Config) WithConsensusParams(params *tmproto.ConsensusParams) *Config {
	c.Genesis = c.Genesis.WithConsensusParams(params)
	return c
}

func (c *Config) WithMaxBytes(maxBytes int64) *Config {
	c.Genesis.ConsensusParams.Block.MaxBytes = maxBytes
	return c
}

// DefaultConfig returns the default configuration of a test node.
func DefaultConfig() *Config {
	cfg := &Config{}
	return cfg.
		WithGenesis(
			genesis.NewDefaultGenesis().
				WithValidators(genesis.NewDefaultValidator(DefaultValidatorAccountName)).
				WithConsensusParams(DefaultConsensusParams()),
		).
		WithTendermintConfig(DefaultTendermintConfig()).
		WithAppCreator(DefaultAppCreator()).
		WithAppConfig(DefaultAppConfig()).
		WithAppOptions(DefaultAppOptions()).
		WithSuppressLogs(true).
		WithDelayedPrecommitTimeout(200 * time.Millisecond)
}

func DefaultConsensusParams() *tmproto.ConsensusParams {
	cparams := app.DefaultConsensusParams()
	cparams.Version.App = appconsts.Version
	return cparams
}

func DefaultTendermintConfig() *tmconfig.Config {
	tmCfg := app.DefaultConsensusConfig()

	// Set all the ports to random open ones.
	tmCfg.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", MustGetFreePort())
	tmCfg.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", MustGetFreePort())
	tmCfg.RPC.GRPCListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", MustGetFreePort())

	tmCfg.TxIndex.Indexer = "kv"

	return tmCfg
}

type AppCreationOptions func(app *app.App)

func DefaultAppCreator(opts ...AppCreationOptions) srvtypes.AppCreator {
	return func(_ log.Logger, _ dbm.DB, _ io.Writer, appOptions srvtypes.AppOptions) srvtypes.Application {
		baseAppOptions := server.DefaultBaseappOptions(appOptions)
		baseAppOptions = append(baseAppOptions, baseapp.SetMinGasPrices(fmt.Sprintf("%v%v", appconsts.DefaultMinGasPrice, appconsts.BondDenom)))

		// Check for the new --block-time flag first, then fall back to deprecated --timeout-commit
		var blockTime time.Duration
		if blockTimeFromFlag := appOptions.Get(DelayedPrecommitTimeout); blockTimeFromFlag != nil {
			blockTime = blockTimeFromFlag.(time.Duration)
		} else if timeoutCommitFromFlag := appOptions.Get(TimeoutCommitFlag); timeoutCommitFromFlag != nil {
			blockTime = timeoutCommitFromFlag.(time.Duration)
		}

		app := app.New(
			log.NewNopLogger(),
			dbm.NewMemDB(),
			nil, // trace store
			blockTime,
			simtestutil.EmptyAppOptions{},
			baseAppOptions...,
		)

		for _, opt := range opts {
			opt(app)
		}

		return app
	}
}

// CustomAppCreator creates a custom application instance using provided baseapp options.
// Returns a function that initializes the app.
func CustomAppCreator(appOptions ...func(*baseapp.BaseApp)) srvtypes.AppCreator {
	return func(log.Logger, dbm.DB, io.Writer, srvtypes.AppOptions) srvtypes.Application {
		return app.New(
			log.NewNopLogger(),
			dbm.NewMemDB(),
			nil, // trace store
			0,   // timeout commit
			simtestutil.EmptyAppOptions{},
			appOptions...,
		)
	}
}

// DefaultAppConfig wraps the default config described in the server
func DefaultAppConfig() *srvconfig.Config {
	appCfg := app.DefaultAppConfig()
	appCfg.GRPC.Enable = true
	appCfg.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", MustGetFreePort())
	appCfg.API.Enable = true
	appCfg.API.Address = fmt.Sprintf("tcp://127.0.0.1:%d", MustGetFreePort())
	return appCfg
}
