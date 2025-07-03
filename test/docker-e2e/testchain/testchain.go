package testchain

import (
	"context"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	"github.com/celestiaorg/tastora/framework/testutil/maps"
	cometcfg "github.com/cometbft/cometbft/config"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	servercfg "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

const (
	multiplexerImage   = "ghcr.io/celestiaorg/celestia-app"
	defaultCelestiaTag = "v4.0.0-rc6"
)

// Config represents the configuration for a docker Celestia setup.
type Config struct {
	*testnode.Config
	Image           string
	Tag             string
	DockerClient    *client.Client
	DockerNetworkID string
}

// DefaultConfig returns a configured instance of Config with a custom genesis and validators for Celestia applications.
func DefaultConfig() Config {
	tnCfg := testnode.DefaultConfig()
	// default + 2 extra validators.
	tnCfg.Genesis = tnCfg.Genesis.
		WithChainID("test").
		WithValidators(
			genesis.NewDefaultValidator("val1"),
			genesis.NewDefaultValidator("val2"),
		)

	return Config{
		Config: tnCfg,
		Image:  getCelestiaImage(),
		Tag:    getCelestiaTag(),
	}
}

// NewCelestiaChainBuilder constructs a new ChainBuilder configured for a Celestia instance with predefined parameters.
func NewCelestiaChainBuilder(t *testing.T, cfg Config) *tastoradockertypes.ChainBuilder {
	genesisBz, err := cfg.Genesis.ExportBytes()
	require.NoError(t, err, "failed to export genesis bytes")

	// TODO: why do I need to do this?
	genesisBz, err = maps.SetField(genesisBz, "consensus", map[string]interface{}{})
	require.NoError(t, err)
	genesisBz, err = maps.SetField(genesisBz, "consensus.params.version.app", "4")
	require.NoError(t, err)

	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

	kr := cfg.Genesis.Keyring()

	// TODO: accountNames can't be hard coded
	accountNames := []string{"validator", "val1", "val2"}
	vals := make([]tastoradockertypes.ChainNodeConfig, 3)
	for i := 0; i < len(accountNames); i++ {
		privKeyBz := getValidatorPrivateKeyBytes(t, cfg.Genesis, i)
		vals[i] = tastoradockertypes.NewChainNodeConfigBuilder().
			WithPrivValidatorKey(privKeyBz).
			WithAccountName(accountNames[i]).
			WithKeyring(kr).
			Build()
	}

	return tastoradockertypes.NewChainBuilder(t).
		WithName("celestia"). // just influences home directory on the host.
		WithChainID(cfg.Genesis.ChainID).
		WithDockerClient(cfg.DockerClient).
		WithDockerNetworkID(cfg.DockerNetworkID).
		WithImage(tastoradockertypes.NewDockerImage(cfg.Image, cfg.Tag, "10001:10001")).
		WithAdditionalStartArgs("--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099").
		WithEncodingConfig(&encodingConfig).
		WithPostInit(getPostInitModifications("0.025utia")...).
		WithNodes(vals...).
		WithGenesis(genesisBz)
}

// getPostInitModifications returns a slice of functions to modify configuration files of a ChainNode post-initialization.
func getPostInitModifications(gasPrices string) []func(context.Context, *tastoradockertypes.ChainNode) error {
	var fns []func(context.Context, *tastoradockertypes.ChainNode) error

	fns = append(fns, func(ctx context.Context, node *tastoradockertypes.ChainNode) error {
		return config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
			cfg.LogLevel = "info"
			cfg.TxIndex.Indexer = "kv"
			cfg.P2P.AllowDuplicateIP = true
			cfg.P2P.AddrBookStrict = false
			blockTime := time.Duration(2) * time.Second
			cfg.Consensus.TimeoutCommit = blockTime
			cfg.Consensus.TimeoutPropose = blockTime
			cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"
			cfg.RPC.CORSAllowedOrigins = []string{"*"}
		})
	})

	fns = append(fns, func(ctx context.Context, node *tastoradockertypes.ChainNode) error {
		return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
			cfg.MinGasPrices = gasPrices
			cfg.GRPC.Address = "0.0.0.0:9090"
			cfg.API.Enable = true
			cfg.API.Swagger = true
			cfg.API.Address = "tcp://0.0.0.0:1317"
		})
	})
	return fns
}

// getValidatorPrivateKeyBytes returns the contents of the priv_validator_key.json file.
func getValidatorPrivateKeyBytes(t *testing.T, genesis *genesis.Genesis, idx int) []byte {
	validator, exists := genesis.Validator(idx)
	require.True(t, exists, "validator at index 0 should exist")
	privValKey := validator.ConsensusKey

	key := privval.FilePVKey{
		Address: privValKey.PubKey().Address(),
		PubKey:  privValKey.PubKey(),
		PrivKey: privValKey,
	}

	privValidatorKeyBz, err := cmtjson.MarshalIndent(key, "", "  ")
	require.NoError(t, err, "failed to marshal priv_validator_key.json")
	return privValidatorKeyBz
}

// SetupTxClient initializes and returns a transaction client for interacting with a chain node using the provided configuration.
func SetupTxClient(ctx context.Context, cn *tastoradockertypes.ChainNode, cfg Config) (*user.TxClient, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	return user.SetupTxClient(
		ctx,
		cfg.Genesis.Keyring(),
		cn.GrpcConn,
		encCfg,
	)
}

// getCelestiaImage returns the image to use for Celestia app.
// It can be overridden by setting the CELESTIA_IMAGE environment.
func getCelestiaImage() string {
	if image := os.Getenv("CELESTIA_IMAGE"); image != "" {
		return image
	}
	return multiplexerImage
}

// getCelestiaTag returns the tag to use for Celestia images.
// It can be overridden by setting the CELESTIA_TAG environment.
func getCelestiaTag() string {
	if tag := os.Getenv("CELESTIA_TAG"); tag != "" {
		return tag
	}
	return defaultCelestiaTag
}
