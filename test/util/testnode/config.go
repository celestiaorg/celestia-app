package testnode

import (
	"time"

	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/util/genesis"
	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	"github.com/cosmos/cosmos-sdk/server"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	tmconfig "github.com/tendermint/tendermint/config"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

const (
	DefaultValidatorAccountName = "validator"
)

// Config is the configuration of a test node.
type Config struct {
	Genesis *genesis.Genesis
	// TmConfig is the Tendermint configuration used for the network.
	TmConfig *tmconfig.Config
	// AppConfig is the application configuration of the test node.
	AppConfig *srvconfig.Config
	// AppOptions are the application options of the test node.
	AppOptions *KVAppOptions
	// AppCreator is used to create the application for the testnode.
	AppCreator srvtypes.AppCreator
	// SupressLogs
	SupressLogs bool
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

// WithSupressLogs sets the SupressLogs and returns the Config.
func (c *Config) WithSupressLogs(sl bool) *Config {
	c.SupressLogs = sl
	return c
}

// WithTimeoutCommit sets the CommitTimeout and returns the Config.
func (c *Config) WithTimeoutCommit(d time.Duration) *Config {
	c.TmConfig.Consensus.TimeoutCommit = d
	return c
}

// WithFundedAccounts sets the genesis accounts and returns the Config.
func (c *Config) WithFundedAccounts(accounts ...string) *Config {
	c.Genesis = c.Genesis.WithAccounts(
		genesis.NewAccounts(999999999999999999, accounts...)...,
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

func DefaultConfig() *Config {
	tmcfg := DefaultTendermintConfig()
	tmcfg.Consensus.TimeoutCommit = 1 * time.Millisecond
	cfg := &Config{}
	return cfg.
		WithGenesis(
			genesis.NewDefaultGenesis().
				WithChainID(tmrand.Str(6)).
				WithGenesisTime(time.Now()).
				WithConsensusParams(DefaultParams()).
				WithModifiers().
				WithValidators(genesis.NewDefaultValidator(DefaultValidatorAccountName)),
		).
		WithTendermintConfig(DefaultTendermintConfig()).
		WithAppConfig(DefaultAppConfig()).
		WithAppOptions(DefaultAppOptions()).
		WithAppCreator(cmd.NewAppServer).
		WithSupressLogs(true)
}

type KVAppOptions struct {
	options map[string]interface{}
}

// Get implements AppOptions
func (ao *KVAppOptions) Get(o string) interface{} {
	return ao.options[o]
}

// Set adds an option to the KVAppOptions
func (ao *KVAppOptions) Set(o string, v interface{}) {
	ao.options[o] = v
}

// DefaultAppOptions returns the default application options.
func DefaultAppOptions() *KVAppOptions {
	opts := &KVAppOptions{options: make(map[string]interface{})}
	opts.Set(server.FlagPruning, pruningtypes.PruningOptionNothing)
	return opts
}

func DefaultParams() *tmproto.ConsensusParams {
	cparams := types.DefaultConsensusParams()
	cparams.Block.TimeIotaMs = 1
	cparams.Block.MaxBytes = appconsts.DefaultMaxBytes
	cparams.Version.AppVersion = appconsts.LatestVersion
	return cparams
}

func DefaultTendermintConfig() *tmconfig.Config {
	tmCfg := tmconfig.DefaultConfig()
	// TimeoutCommit is the duration the node waits after committing a block
	// before starting the next height. This duration influences the time
	// interval between blocks. A smaller TimeoutCommit value could lead to
	// less time between blocks (i.e. shorter block intervals).
	tmCfg.Consensus.TimeoutCommit = 1 * time.Millisecond

	// set the mempool's MaxTxBytes to allow the testnode to accept a
	// transaction that fills the entire square. Any blob transaction larger
	// than the square size will still fail no matter what.
	upperBoundBytes := appconsts.DefaultSquareSizeUpperBound * appconsts.DefaultSquareSizeUpperBound * appconsts.ContinuationSparseShareContentSize
	tmCfg.Mempool.MaxTxBytes = upperBoundBytes

	// remove all barriers from the testnode being able to accept very large
	// transactions and respond to very queries with large responses (~200MB was
	// chosen only as an arbitrary large number).
	tmCfg.RPC.MaxBodyBytes = 200_000_000

	return tmCfg
}
