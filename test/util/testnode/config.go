package testnode

import (
	"fmt"
	"io"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/cosmos/cosmos-sdk/baseapp"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

const (
	kibibyte                    = 1024      // bytes
	mebibyte                    = 1_048_576 // bytes
	DefaultValidatorAccountName = "validator"
	DefaultInitialBalance       = genesis.DefaultInitialBalance
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

// WithBlockTime sets the block time and returns the Config.
func (c *Config) WithBlockTime(d time.Duration) *Config {
	c.TmConfig.Consensus.TimeoutCommit = d
	creator := DefaultAppCreator(d)
	c.WithAppCreator(creator)
	return c
}

// WithFundedAccounts sets the genesis accounts and returns the Config.
func (c *Config) WithFundedAccounts(accounts ...string) *Config {
	c.Genesis = c.Genesis.WithKeyringAccounts(
		genesis.NewKeyringAccounts(DefaultInitialBalance, accounts...)...,
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
		WithAppConfig(DefaultAppConfig()).
		WithAppOptions(DefaultAppOptions()).
		WithAppCreator(DefaultAppCreator(time.Millisecond * 30)).
		WithSuppressLogs(true)
}

func DefaultConsensusParams() *tmproto.ConsensusParams {
	cparams := types.DefaultConsensusParams()
	cparams.Block.TimeIotaMs = 1
	cparams.Block.MaxBytes = appconsts.DefaultMaxBytes
	cparams.Version.AppVersion = appconsts.LatestVersion
	return cparams
}

func DefaultTendermintConfig() *tmconfig.Config {
	tmCfg := tmconfig.DefaultConfig()
	// Reduce the timeout commit to 1ms to speed up the rate at which the test
	// node produces blocks.
	tmCfg.Consensus.TimeoutCommit = 1 * time.Millisecond

	// Override the mempool's MaxTxBytes to allow the testnode to accept a
	// transaction that fills the entire square. Any blob transaction larger
	// than the square size will still fail no matter what.
	maxTxBytes := appconsts.DefaultUpperBoundMaxBytes
	tmCfg.Mempool.MaxTxBytes = maxTxBytes

	// Override the MaxBodyBytes to allow the testnode to accept very large
	// transactions and respond to queries with large responses (200 MiB was
	// chosen only as an arbitrary large number).
	tmCfg.RPC.MaxBodyBytes = 200 * mebibyte

	tmCfg.RPC.TimeoutBroadcastTxCommit = time.Minute

	// set all the ports to random open ones
	tmCfg.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", mustGetFreePort())
	tmCfg.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", mustGetFreePort())
	tmCfg.RPC.GRPCListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", mustGetFreePort())

	return tmCfg
}

func DefaultAppCreator(blockTime time.Duration) srvtypes.AppCreator {
	return func(_ log.Logger, _ tmdb.DB, _ io.Writer, _ srvtypes.AppOptions) srvtypes.Application {
		encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
		app := app.New(
			log.NewNopLogger(),
			tmdb.NewMemDB(),
			nil, // trace store
			0,   // invCheckPerid
			encodingConfig,
			0, // v2 upgrade height
			simapp.EmptyAppOptions{},
			baseapp.SetMinGasPrices(fmt.Sprintf("%v%v", appconsts.DefaultMinGasPrice, app.BondDenom)),
		)
		app.SetEndBlocker(wrapEndBlocker(app, blockTime))
		return app
	}
}

func CustomAppCreator(minGasPrice string) srvtypes.AppCreator {
	return func(_ log.Logger, _ tmdb.DB, _ io.Writer, _ srvtypes.AppOptions) srvtypes.Application {
		encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
		app := app.New(
			log.NewNopLogger(),
			tmdb.NewMemDB(),
			nil, // trace store
			0,   // invCheckPerid
			encodingConfig,
			0, // v2 upgrade height
			simapp.EmptyAppOptions{},
			baseapp.SetMinGasPrices(minGasPrice),
		)
		app.SetEndBlocker(wrapEndBlocker(app, time.Millisecond*0))
		return app
	}
}

// DefaultAppConfig wraps the default config described in the server
func DefaultAppConfig() *srvconfig.Config {
	appCfg := srvconfig.DefaultConfig()
	appCfg.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", mustGetFreePort())
	appCfg.API.Address = fmt.Sprintf("tcp://127.0.0.1:%d", mustGetFreePort())
	appCfg.MinGasPrices = fmt.Sprintf("%v%s", appconsts.DefaultMinGasPrice, appconsts.BondDenom)
	return appCfg
}
