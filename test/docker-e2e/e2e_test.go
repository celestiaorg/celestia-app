package docker_e2e

import (
	"context"
	"fmt"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/go-square/v2/share"
	celestiadockertypes "github.com/chatton/celestia-test/framework/docker"
	"github.com/chatton/celestia-test/framework/testutil/maps"
	celestiatypes "github.com/chatton/celestia-test/framework/types"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"os"
	"testing"
	"time"
)

const (
	multiplexerImage   = "ghcr.io/celestiaorg/celestia-app"
	txsimImage         = "ghcr.io/celestiaorg/txsim"
	defaultCelestiaTag = "v4.0.0-rc4"
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
	s.logger.Info("Setting up Celestia test suite")
	s.client, s.network = celestiadockertypes.DockerSetup(s.T())
}

func (s *CelestiaTestSuite) CreateDockerProvider(appVersion string) celestiatypes.Provider {
	numValidators := 4
	numFullNodes := 0

	enc := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

	cfg := celestiadockertypes.Config{
		Logger:          s.logger,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
		ChainConfig: &celestiadockertypes.ChainConfig{
			Type:          "cosmos",
			Name:          "celestia",
			Version:       getCelestiaTag(),
			NumValidators: &numValidators,
			NumFullNodes:  &numFullNodes,
			ChainID:       "celestia",
			Images: []celestiadockertypes.DockerImage{
				{
					Repository: multiplexerImage,
					Version:    getCelestiaTag(),
					UIDGID:     "10001:10001",
				},
			},
			Bin:           "celestia-appd",
			Bech32Prefix:  "celestia",
			Denom:         "utia",
			CoinType:      "118",
			GasPrices:     "0.025utia",
			GasAdjustment: 1.3,
			ModifyGenesis: func(config celestiadockertypes.Config, bytes []byte) ([]byte, error) {
				return maps.SetField(bytes, "consensus.params.version.app", appVersion)
			},
			EncodingConfig:      &enc,
			AdditionalStartArgs: []string{"--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9098"},
		},
		BridgeNodeConfig: &celestiadockertypes.BridgeNodeConfig{
			ChainID: "celestia",
			Images: []celestiadockertypes.DockerImage{
				{
					Repository: "ghcr.io/celestiaorg/celestia-node",
					Version:    "v0.23.0-rc0", // TODO: this should be configurable.
					UIDGID:     "10001:10001",
				},
			}},
	}
	return celestiadockertypes.NewProvider(cfg, s.T().Name())
}

// CreateTxSim deploys and starts a txsim container to simulate transactions against the given celestia chain in the test environment.
func (s *CelestiaTestSuite) CreateTxSim(ctx context.Context, chain celestiatypes.Chain) {
	t := s.T()
	networkName, err := getNetworkNameFromID(ctx, s.client, s.network)
	s.Require().NoError(err)

	// Deploy txsim image
	t.Log("Deploying txsim image")
	txsimImage := celestiadockertypes.NewImage(s.logger, s.client, networkName, t.Name(), txsimImage, getCelestiaTag())

	opts := celestiadockertypes.ContainerOptions{
		User: "0:0",
		// Mount the Celestia home directory into the txsim container
		// this ensures txsim has access to a keyring and is able to broadcast transactions.
		Binds: []string{chain.GetVolumeName() + ":/celestia-home"},
	}

	args := []string{
		"/bin/txsim",
		"--key-path", "/celestia-home",
		"--grpc-endpoint", chain.GetGRPCAddress(),
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

	// Cleanup the container when the test is done
	t.Cleanup(func() {
		if err := container.Stop(10 * time.Second); err != nil {
			t.Logf("Error stopping txsim container: %v", err)
		}
	})
}

// getGenesisHash returns the genesis hash of the given chain node.
func (s *CelestiaTestSuite) getGenesisHash(ctx context.Context, node celestiatypes.ChainNode) string {
	c, err := node.GetRPCClient()
	s.Require().NoError(err, "failed to get node client")

	first := int64(1)
	block, err := c.Block(ctx, &first)
	s.Require().NoError(err, "failed to get block")

	return block.Block.Header.Hash().String()
}

// getNetworkNameFromID resolves the network name given its ID.
func getNetworkNameFromID(ctx context.Context, cli *client.Client, networkID string) (string, error) {
	network, err := cli.NetworkInspect(ctx, networkID, dockertypes.NetworkInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to inspect network %s: %w", networkID, err)
	}
	if network.Name == "" {
		return "", fmt.Errorf("network %s has no name", networkID)
	}
	return network.Name, nil
}

// getDockerRegistry returns the Docker registry to use for images.
// It can be overridden by setting the DOCKER_REGISTRY environment variable.
// If no override is provided, it returns the default "ghcr.io/celestiaorg".
func getDockerRegistry() string {
	if registry := os.Getenv("DOCKER_REGISTRY"); registry != "" {
		return registry
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
