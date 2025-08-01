package dockerchain

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	"github.com/celestiaorg/tastora/framework/testutil/maps"
	cometcfg "github.com/cometbft/cometbft/config"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	servercfg "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"
)

// NewCelestiaChainBuilder constructs a new ChainBuilder configured for a Celestia instance with predefined parameters.
func NewCelestiaChainBuilder(t *testing.T, cfg *Config) *tastoradockertypes.ChainBuilder {
	genesisBz, err := cfg.Genesis.ExportBytes()
	require.NoError(t, err, "failed to export genesis bytes")

	// TODO: why do I need to do this?
	// fails with 2025-07-03 14:57:42 Error: failed to get current app state: failed to determine genesis version
	genesisBz, err = maps.SetField(genesisBz, "consensus.params.version.app", "4")
	require.NoError(t, err)

	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

	kr := cfg.Genesis.Keyring()

	records, err := kr.List()
	require.NoError(t, err)

	vals := make([]tastoradockertypes.ChainNodeConfig, len(records))
	for i, record := range records {
		val, exists := cfg.Genesis.Validator(i)
		require.True(t, exists, "validator at index %d should exist", i)
		privKeyBz, err := getPrivValidatorKeyJsonBytes(val.PrivateKey())
		require.NoError(t, err, "failed to get validator private key bytes")

		vals[i] = tastoradockertypes.NewChainNodeConfigBuilder().
			WithPrivValidatorKey(privKeyBz).
			WithAccountName(record.Name).
			WithKeyring(kr).
			Build()
	}

	// register the first wallet as the faucet wallet
	addr, err := records[0].GetAddress()
	require.NoError(t, err, "failed to get address from keyring record")
	// Create the wallet with all required fields
	faucetWallet := tastoradockertypes.NewWallet(addr, addr.String(), "celestia", records[0].Name)

	return tastoradockertypes.NewChainBuilder(t).
		WithName("celestia"). // just influences home directory on the host.
		WithChainID(cfg.Genesis.ChainID).
		WithDockerClient(cfg.DockerClient).
		WithDockerNetworkID(cfg.DockerNetworkID).
		WithImage(tastoracontainertypes.NewImage(cfg.Image, cfg.Tag, "10001:10001")).
		WithAdditionalStartArgs("--force-no-bbr").
		WithEncodingConfig(&encodingConfig).
		WithPostInit(getPostInitModifications("0.025utia")...).
		WithFaucetWallet(faucetWallet).
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
			cfg.RPC.GRPCListenAddress = "tcp://0.0.0.0:9099"
			cfg.RPC.CORSAllowedOrigins = []string{"*"}
		})
	})

	fns = append(fns, func(ctx context.Context, node *tastoradockertypes.ChainNode) error {
		return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
			cfg.MinGasPrices = gasPrices
			cfg.GRPC.Address = "0.0.0.0:9090"
			cfg.GRPC.Enable = true
			cfg.API.Enable = true
			cfg.API.Swagger = true
			cfg.API.Address = "tcp://0.0.0.0:1317"
		})
	})
	return fns
}

// SetupTxClient initializes and returns a transaction client for interacting with a chain node using the provided configuration.
func SetupTxClient(ctx context.Context, cn *tastoradockertypes.ChainNode, cfg *Config) (*user.TxClient, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	return user.SetupTxClient(
		ctx,
		cfg.Genesis.Keyring(),
		cn.GrpcConn,
		encCfg,
	)
}

// NodeConfigBuilders returns a list of ChainNodeConfigBuilder and any error if one occurs.
// This function populates a default set of ChainNodeConfigBuilder based on the provided config.
// This handles the population of private key bytes, record name and keyring.
//
// If any custom modifications are required to any individual validator, this function can be used
// to modify the ChainNodeConfigBuilder.
//
// Example:
//
//	for i, nodeBuilder := range nodeBuilders {
//	    version := getVersionForIndex(i)
//		nodeBuilder.WithImage(tastoradockertypes.NewDockerImage(cfg.Image, version, "10001:10001")
//	}
func NodeConfigBuilders(cfg *Config) ([]*tastoradockertypes.ChainNodeConfigBuilder, error) {
	kr := cfg.Genesis.Keyring()
	records, err := kr.List()
	if err != nil {
		return nil, err
	}

	chainNodeBuilders := make([]*tastoradockertypes.ChainNodeConfigBuilder, len(records))
	for i, record := range records {
		validator, exists := cfg.Genesis.Validator(i)
		if !exists {
			return nil, fmt.Errorf("validator at index %d should exist", i)
		}
		privKeyBz, err := getPrivValidatorKeyJsonBytes(validator.PrivateKey())
		if err != nil {
			return nil, fmt.Errorf("failed to get validator private key bytes: %w", err)
		}

		chainNodeBuilders[i] = tastoradockertypes.NewChainNodeConfigBuilder().
			WithPrivValidatorKey(privKeyBz).
			WithAccountName(record.Name).
			WithImage(tastoracontainertypes.NewImage(cfg.Image, cfg.Tag, "10001:10001")).
			WithKeyring(kr)
	}

	return chainNodeBuilders, nil
}

// getPrivValidatorKeyJsonBytes marshals a FilePVKey into an indented JSON byte slice or returns an error if marshalling fails.
// the contents can directly be used as the contents of priv_validator_key.json
func getPrivValidatorKeyJsonBytes(key privval.FilePVKey) ([]byte, error) {
	privValidatorKeyBz, err := cmtjson.MarshalIndent(key, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling priv_validator_key.json: %w", err)
	}
	return privValidatorKeyBz, err
}
