package docker_e2e

import (
	"context"
	"fmt"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	"testing"
	"time"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"github.com/celestiaorg/go-square/v2/share"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/docker/docker/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	txsimImage = "ghcr.io/celestiaorg/txsim"
	txSimTag   = "v4.0.10-arabica"
	homeDir    = "/var/cosmos-chain/celestia"
)

func TestCelestiaTestSuite(t *testing.T) {
	suite.Run(t, new(CelestiaTestSuite))
}

type CelestiaTestSuite struct {
	suite.Suite
	logger     *zap.Logger
	client     *client.Client
	network    string
	celestiaCfg *dockerchain.Config // Config used to build the celestia chain, needed for upgrades
}

func (s *CelestiaTestSuite) SetupSuite() {
	s.logger = zaptest.NewLogger(s.T())
	s.logger.Info("Setting up Celestia test suite: " + s.T().Name())
	s.client, s.network = tastoradockertypes.DockerSetup(s.T())
}

// CreateTxSim deploys and starts a txsim container to simulate transactions against the given celestia chain in the test environment.
func (s *CelestiaTestSuite) CreateTxSim(ctx context.Context, chain tastoratypes.Chain) {
	t := s.T()
	networkName, err := getNetworkNameFromID(ctx, s.client, s.network)
	s.Require().NoError(err)

	// Deploy txsim image
	t.Log("Deploying txsim image")
	txsimImage := tastoracontainertypes.NewJob(s.logger, s.client, networkName, t.Name(), txsimImage, txSimTag)

	opts := tastoracontainertypes.Options{
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

// GetLatestBlockHeight returns the latest block height of the given node.
// This function will periodically check for the latest block height until the timeout is reached.
// If the timeout is reached, an error will be returned.
func (s *CelestiaTestSuite) GetLatestBlockHeight(ctx context.Context, statusClient rpcclient.StatusClient) (int64, error) {
	// use a ticker to periodically check for the initial height
	heightTicker := time.NewTicker(1 * time.Second)
	defer heightTicker.Stop()

	heightTimeoutCtx, heightCancel := context.WithTimeout(ctx, 30*time.Second)
	defer heightCancel()

	// check immediately first, then on ticker intervals
	for {
		status, err := statusClient.Status(heightTimeoutCtx)
		if err == nil && status.SyncInfo.LatestBlockHeight > 0 {
			return status.SyncInfo.LatestBlockHeight, nil
		}

		select {
		case <-heightTicker.C:
			// continue the loop
		case <-heightTimeoutCtx.Done():
			return 0, fmt.Errorf("timed out waiting for initial height")
		}
	}
}

// WaitForSync waits for a Celestia node to synchronize based on a provided sync condition within a specified timeout.
// The method periodically checks the node's sync status. Returns an error if the timeout is exceeded.
// Returns nil when the provided condition function returns true.
func (s *CelestiaTestSuite) WaitForSync(ctx context.Context, statusClient rpcclient.StatusClient, syncTimeout time.Duration, syncCondition func(coretypes.SyncInfo) bool) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	s.T().Log("Waiting for sync to complete...")

	// check immediately first
	if status, err := statusClient.Status(timeoutCtx); err == nil {
		s.T().Logf("Sync node status: Height=%d, CatchingUp=%t", status.SyncInfo.LatestBlockHeight, status.SyncInfo.CatchingUp)
		if syncCondition(status.SyncInfo) {
			s.T().Logf("Sync completed successfully")
			return nil
		}
	}

	// then check on ticker intervals
	for {
		select {
		case <-ticker.C:
			status, err := statusClient.Status(timeoutCtx)
			if err != nil {
				s.T().Logf("Failed to get status from state sync node, retrying...: %v", err)
				continue
			}

			s.T().Logf("Sync node status: Height=%d, CatchingUp=%t", status.SyncInfo.LatestBlockHeight, status.SyncInfo.CatchingUp)

			if syncCondition(status.SyncInfo) {
				s.T().Logf("Sync completed successfully")
				return nil
			}

		case <-timeoutCtx.Done():
			return fmt.Errorf("timed out waiting for state sync node to catch up after %v", syncTimeout)
		}
	}
}
