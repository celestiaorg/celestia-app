package docker_e2e

import (
	"context"
	"fmt"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	cometcfg "github.com/cometbft/cometbft/config"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	servercfg "github.com/cosmos/cosmos-sdk/server/config"
	"strings"

	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/celestiaorg/tastora/framework/testutil/maps"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/go-square/v2/share"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/docker/docker/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	multiplexerImage   = "ghcr.io/celestiaorg/celestia-app"
	txsimImage         = "ghcr.io/celestiaorg/txsim"
	defaultCelestiaTag = "v4.0.0-rc6"
	txSimTag           = "v4.0.0-rc6"
)

func TestCelestiaTestSuite(t *testing.T) {
	suite.Run(t, new(CelestiaTestSuite))
}

type CelestiaTestSuite struct {
	suite.Suite
	logger  *zap.Logger
	client  *client.Client
	network string
}

func (s *CelestiaTestSuite) SetupSuite() {
	s.logger = zaptest.NewLogger(s.T())
	s.logger.Info("Setting up Celestia test suite: " + s.T().Name())
	s.client, s.network = tastoradockertypes.DockerSetup(s.T())
}

func (s *CelestiaTestSuite) SetupTest() {
	s.T().Log("Setting up Celestia test: " + s.T().Name())
}

// Builder returns a ChainBuilder that provides all of the default configurations for a celestia app chain.
// any changes can be made with the builder before calling Build based on the requirements of given test.
func (s *CelestiaTestSuite) Builder() *tastoradockertypes.ChainBuilder {
	// default + 2 extra validators.
	g := testnode.DefaultConfig().Genesis.WithChainID("celestia").WithValidators(
		genesis.NewDefaultValidator("val1"),
		genesis.NewDefaultValidator("val2"),
	)

	genesisBz, err := g.ExportBytes()
	s.Require().NoError(err, "failed to export genesis bytes")

	// TODO: why do I need to do this?
	genesisBz, err = maps.SetField(genesisBz, "consensus", map[string]interface{}{})
	s.Require().NoError(err)
	genesisBz, err = maps.SetField(genesisBz, "consensus.params.version.app", "4")
	s.Require().NoError(err)

	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

	client, network := tastoradockertypes.DockerSetup(s.T())

	kr := g.Keyring()

	accountNames := []string{"validator", "val1", "val2"}
	vals := make([]tastoradockertypes.ChainNodeConfig, 3)
	for i := 0; i < 3; i++ {
		privKeyBz := getValidatorPrivateKeyBytes(s.T(), g, i)
		vals[i] = tastoradockertypes.NewChainNodeConfigBuilder().
			WithPrivValidatorKey(privKeyBz).
			WithAccountName(accountNames[i]).
			WithKeyring(kr).
			Build()
	}

	return tastoradockertypes.NewChainBuilder(s.T()).
		WithName("celestia"). // just influences home directory on the host.
		WithChainID(g.ChainID).
		WithDockerClient(client).
		WithDockerNetworkID(network).
		WithImage(tastoradockertypes.NewDockerImage(getCelestiaImage(), getCelestiaTag(), "10001:10001")).
		WithAdditionalStartArgs("--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099").
		WithEncodingConfig(&encodingConfig).
		WithPostInit(getPostInitModifications("0.025utia")...).
		WithNodes(vals...).
		WithGenesis(genesisBz)
}

// CreateTxSim deploys and starts a txsim container to simulate transactions against the given celestia chain in the test environment.
func (s *CelestiaTestSuite) CreateTxSim(ctx context.Context, chain tastoratypes.Chain) {
	t := s.T()
	networkName, err := getNetworkNameFromID(ctx, s.client, s.network)
	s.Require().NoError(err)

	// Deploy txsim image
	t.Log("Deploying txsim image")
	txsimImage := tastoradockertypes.NewImage(s.logger, s.client, networkName, t.Name(), txsimImage, txSimTag)

	opts := tastoradockertypes.ContainerOptions{
		User: "0:0",
		// Mount the Celestia home directory into the txsim container
		// this ensures txsim has access to a keyring and is able to broadcast transactions.
		Binds: []string{chain.GetVolumeName() + ":/celestia-home"},
	}

	hostGRPCPort := strings.Split(chain.GetGRPCAddress(), ":")[1]

	args := []string{
		"/bin/txsim",
		"--key-path", "/celestia-home",
		"--grpc-endpoint", "host.docker.internal:" + hostGRPCPort,
		"--poll-time", "1s",
		"--seed", "42",
		"--blob", "10",
		"--blob-amounts", "100",
		"--blob-sizes", "100-2000",
		"--gas-price", "0.025",
		"--blob-share-version", fmt.Sprintf("%d", share.ShareVersionZero),
	}

	// Start the txsim container
	container, err := txsimImage.Start(ctx, args, opts)
	require.NoError(t, err, "Failed to start txsim container")
	t.Log("TxSim container started successfully")
	t.Logf("TxSim container ID: %s", container.Name)

	// cleanup the container when the test is done
	t.Cleanup(func() {
		if err := container.Stop(10 * time.Second); err != nil {
			t.Logf("Error stopping txsim container: %v", err)
		}
	})
}

// getNetworkNameFromID resolves the network name given its ID.
func getNetworkNameFromID(ctx context.Context, cli *client.Client, networkID string) (string, error) {
	network, err := cli.NetworkInspect(ctx, networkID, network.InspectOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to inspect network %s: %w", networkID, err)
	}
	if network.Name == "" {
		return "", fmt.Errorf("network %s has no name", networkID)
	}
	return network.Name, nil
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
