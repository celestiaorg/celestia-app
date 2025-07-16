package docker_e2e

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v5/pkg/appconsts"
	"github.com/celestiaorg/go-square/v2/share"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/docker/docker/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	txsimImage = "ghcr.io/celestiaorg/txsim"
	txSimTag   = "v4.0.7-mocha"
	homeDir    = "/var/cosmos-chain/celestia"
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

// ExecuteNodeCommand executes a command on a chain node with common parameters automatically added.
// This reduces boilerplate in tests by setting common flags like --home, --node, --fees, etc.
func (s *CelestiaTestSuite) ExecuteNodeCommand(ctx context.Context, chainNode tastoratypes.ChainNode, cmd ...string) (string, string, error) {
	s.Require().Greater(len(cmd), 0, "command must not be empty")

	var finalCmd []string
	isTxCommand := len(cmd) > 0 && cmd[0] == "tx"

	// Common flags for all commands
	if !slices.Contains(cmd, "--home") {
		finalCmd = append(finalCmd, "--home", homeDir)
	}

	if !slices.Contains(cmd, "--node") {
		hostname, err := chainNode.GetInternalHostName(ctx)
		if err != nil {
			return "", "", err
		}
		finalCmd = append(finalCmd, "--node", fmt.Sprintf("tcp://%s:26657", hostname))
	}

	if !slices.Contains(cmd, "--chain-id") {
		finalCmd = append(finalCmd, "--chain-id", appconsts.TestChainID)
	}

	// Transaction-specific flags
	if isTxCommand {
		if !slices.Contains(cmd, "--fees") {
			finalCmd = append(finalCmd, "--fees", "200000utia")
		}

		if !slices.Contains(cmd, "--keyring-backend") {
			finalCmd = append(finalCmd, "--keyring-backend", "test")
		}

		if !slices.Contains(cmd, "--yes") {
			finalCmd = append(finalCmd, "--yes")
		}
	}

	finalCmd = append(cmd, finalCmd...)

	if finalCmd[0] != "celestia-appd" {
		finalCmd = append([]string{"celestia-appd"}, finalCmd...)
	}

	stdoutBytes, stderrBytes, err := chainNode.Exec(ctx, finalCmd, nil)
	return string(stdoutBytes), string(stderrBytes), err
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

	internalHostname, err := chain.GetNodes()[0].GetInternalHostName(ctx)
	s.Require().NoError(err)

	args := []string{
		"/bin/txsim",
		"--key-path", "/celestia-home",
		"--grpc-endpoint", internalHostname + ":9090",
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
