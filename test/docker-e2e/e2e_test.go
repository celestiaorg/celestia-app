package docker_e2e

import (
	"context"
	"fmt"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"strings"

	//"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/celestiaorg/tastora/framework/testutil/maps"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/go-square/v2/share"
	celestiadockertypes "github.com/celestiaorg/tastora/framework/docker"
	celestiatypes "github.com/celestiaorg/tastora/framework/types"
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
	s.client, s.network = celestiadockertypes.DockerSetup(s.T())
}

func (s *CelestiaTestSuite) SetupTest() {
	s.T().Log("Setting up Celestia test: " + s.T().Name())
}

func (s *CelestiaTestSuite) CreateBuilder() *celestiadockertypes.ChainBuilder {
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

	client, network := celestiadockertypes.DockerSetup(s.T())

	kr := g.Keyring()

	accountNames := []string{"validator", "val1", "val2"}
	vals := make([]celestiadockertypes.ChainNodeConfig, 3)
	for i := 0; i < 3; i++ {
		privKeyBz := getValidatorPrivateKeyBytes(s.T(), g, i)
		vals[i] = celestiadockertypes.NewChainNodeConfigBuilder().
			WithPrivValidatorKey(privKeyBz).
			WithAccountName(accountNames[i]).
			WithKeyring(kr).
			Build()
	}

	return celestiadockertypes.NewChainBuilder(s.T()).
		WithName("celestia"). // just influences home directory on the host.
		WithChainID(g.ChainID).
		WithDockerClient(client).
		WithDockerNetworkID(network).
		WithImage(celestiadockertypes.NewDockerImage("ghcr.io/celestiaorg/celestia-app", "v4.0.4-alpha", "10001:10001")).
		WithAdditionalStartArgs("--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099").
		WithEncodingConfig(&encodingConfig).
		WithPostInit(getPostInitModifications("0.025utia")...).
		WithNodes(vals...).
		WithGenesis(genesisBz)
}

// CreateTxSim deploys and starts a txsim container to simulate transactions against the given celestia chain in the test environment.
func (s *CelestiaTestSuite) CreateTxSim(ctx context.Context, chain celestiatypes.Chain) {
	// wait for GRPC server to be ready
	s.Require().NoError(s.waitForGRPC(ctx, chain.GetGRPCAddress()))

	t := s.T()
	networkName, err := getNetworkNameFromID(ctx, s.client, s.network)
	s.Require().NoError(err)

	// Deploy txsim image
	t.Log("Deploying txsim image")
	txsimImage := celestiadockertypes.NewImage(s.logger, s.client, networkName, t.Name(), txsimImage, txSimTag)

	opts := celestiadockertypes.ContainerOptions{
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

// waitForGRPC waits for the GRPC server to be ready to accept connections
func (s *CelestiaTestSuite) waitForGRPC(ctx context.Context, grpcAddr string) error {
	timeout := 30 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		conn, err := grpc.DialContext(ctx, grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("GRPC server at %s not ready after %v", grpcAddr, timeout)
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
