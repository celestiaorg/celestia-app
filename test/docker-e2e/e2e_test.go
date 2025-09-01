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
	"github.com/celestiaorg/tastora/framework/testutil/wait"
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

	// Liveness check constants
	defaultBlocksPerValidator = 3 // Minimum blocks each validator should propose for liveness validation
)

func TestCelestiaTestSuite(t *testing.T) {
	suite.Run(t, new(CelestiaTestSuite))
}

type CelestiaTestSuite struct {
	suite.Suite
	logger      *zap.Logger
	client      *client.Client
	network     string
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

// CheckLiveness validates that all validators proposed blocks and no nodes halted.
// Automatically waits for sufficient blocks (3 per validator minimum) if needed.
//
// Upgrade-agnostic: can be called before/after upgrades or spanning the entire period.
// Call at the end of E2E tests to validate network health.
func (s *CelestiaTestSuite) CheckLiveness(ctx context.Context, chain tastoratypes.Chain) error {
	rpcClient, err := chain.GetNodes()[0].GetRPCClient()
	if err != nil {
		return fmt.Errorf("failed to get RPC client: %w", err)
	}

	endHeight, err := s.ensureMinimumBlocks(ctx, chain, rpcClient, 1)
	if err != nil {
		return fmt.Errorf("failed to ensure minimum blocks: %w", err)
	}

	proposers, err := s.fetchProposerAddresses(ctx, rpcClient, 1, endHeight)
	if err != nil {
		return fmt.Errorf("failed to fetch proposer addresses: %w", err)
	}

	endValidators, err := s.fetchValidatorSets(ctx, rpcClient, endHeight)
	if err != nil {
		return fmt.Errorf("failed to fetch validator sets: %w", err)
	}

	if err := s.validateAllValidatorsProposed(endValidators, proposers, endHeight); err != nil {
		return err
	}

	if err := s.validateNodesNotHalted(ctx, chain, endHeight); err != nil {
		return err
	}

	s.T().Logf("Liveness check passed: all validators proposed blocks and all nodes reached height %d", endHeight)
	return nil
}

// ensureMinimumBlocks waits for additional blocks if necessary to meet the minimum
// requirement of defaultBlocksPerValidator (3) blocks per validator since startHeight.
func (s *CelestiaTestSuite) ensureMinimumBlocks(ctx context.Context, chain tastoratypes.Chain, rpcClient rpcclient.Client, startHeight int64) (int64, error) {
	currentStatus, err := rpcClient.Status(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get current status: %w", err)
	}
	currentHeight := currentStatus.SyncInfo.LatestBlockHeight
	blocksProduced := currentHeight - startHeight

	// Get validator count to calculate minimum required blocks
	validators, err := rpcClient.Validators(ctx, &currentHeight, nil, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get validators: %w", err)
	}
	numValidators := len(validators.Validators)
	minBlocksRequired := int64(numValidators * defaultBlocksPerValidator)

	if blocksProduced >= minBlocksRequired {
		s.T().Logf("Minimum block requirement already met: %d blocks ≥ %d required (%d validators × %d blocks each)", blocksProduced, minBlocksRequired, numValidators, defaultBlocksPerValidator)
		return currentHeight, nil
	}

	additionalBlocksNeeded := minBlocksRequired - blocksProduced
	s.T().Logf("Waiting for %d more blocks to meet minimum requirement (%d produced, %d required for %d validators × %d blocks each)", additionalBlocksNeeded, blocksProduced, minBlocksRequired, numValidators, defaultBlocksPerValidator)

	if err := wait.ForBlocks(ctx, int(additionalBlocksNeeded), chain); err != nil {
		return 0, fmt.Errorf("failed to wait for additional blocks: %w", err)
	}

	finalStatus, err := rpcClient.Status(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get final status: %w", err)
	}
	return finalStatus.SyncInfo.LatestBlockHeight, nil
}

// fetchValidatorSets retrieves validator sets at both start and end heights
func (s *CelestiaTestSuite) fetchValidatorSets(ctx context.Context, rpcClient rpcclient.Client, endHeight int64) (*coretypes.ResultValidators, error) {
	endValidators, err := rpcClient.Validators(ctx, &endHeight, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("validators query at end height %d: %w", endHeight, err)
	}

	return endValidators, nil
}

// fetchProposerAddresses gathers proposer addresses from block headers using efficient batching
func (s *CelestiaTestSuite) fetchProposerAddresses(ctx context.Context, rpcClient rpcclient.Client, startHeight, endHeight int64) ([]string, error) {
	var (
		proposers = make(map[string]struct{})

		// Check blocks after startHeight (exclusive) through endHeight (inclusive)
		minHeight = startHeight + 1
		maxHeight = endHeight
	)

	for minHeight <= maxHeight {
		// BlockchainInfo returns headers in descending order and may limit the range
		blockchainInfo, err := rpcClient.BlockchainInfo(ctx, minHeight, maxHeight)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch block headers for range %d to %d: %w", minHeight, maxHeight, err)
		}

		if len(blockchainInfo.BlockMetas) == 0 {
			return nil, fmt.Errorf("no block headers returned for range %d to %d", minHeight, maxHeight)
		}

		for _, blockMeta := range blockchainInfo.BlockMetas {
			addr := blockMeta.Header.ProposerAddress.String()
			proposers[addr] = struct{}{}
		}

		// Continue from the lowest height we just processed
		// BlockMetas are in descending order, so the last one has the lowest height
		lastProcessedHeight := blockchainInfo.BlockMetas[len(blockchainInfo.BlockMetas)-1].Header.Height

		// If we've processed all requested heights, break
		if lastProcessedHeight <= minHeight {
			break
		}

		// Move to the next batch
		maxHeight = lastProcessedHeight - 1
	}

	// Convert map to slice for cleaner return type
	result := make([]string, 0, len(proposers))
	for addr := range proposers {
		result = append(result, addr)
	}

	blocksProduced := endHeight - startHeight
	s.T().Logf("Found %d unique proposers across %d blocks", len(result), blocksProduced)
	return result, nil
}

// validateAllValidatorsProposed ensures every validator proposed at least one block
func (s *CelestiaTestSuite) validateAllValidatorsProposed(endValidators *coretypes.ResultValidators, proposerAddresses []string, endHeight int64) error {
	proposers := make(map[string]struct{}, len(proposerAddresses))
	for _, addr := range proposerAddresses {
		proposers[addr] = struct{}{}
	}

	allValidators := make(map[string]struct{})

	for _, val := range endValidators.Validators {
		addr := val.Address.String()
		allValidators[addr] = struct{}{}
	}

	s.T().Logf("Checking %d total validators for proposer activity end height %d", len(allValidators), endHeight)

	var missingValidators []string
	for validatorAddr := range allValidators {
		if _, ok := proposers[validatorAddr]; !ok {
			missingValidators = append(missingValidators, validatorAddr)
		}
	}

	if len(missingValidators) > 0 {
		return fmt.Errorf("%d validator(s) never proposed blocks for %d heights", len(missingValidators), endHeight)
	}

	return nil
}

// validateNodesNotHalted ensures no validator nodes halted below the expected height.
// Only validator nodes are checked as non-validator nodes may legitimately be at different heights,
// especially when added during the test (e.g., state sync nodes, full nodes).
func (s *CelestiaTestSuite) validateNodesNotHalted(ctx context.Context, chain tastoratypes.Chain, endHeight int64) error {
	var haltedNodes []string
	for i, n := range chain.GetNodes() {
		// Only check validator nodes for height consistency
		if n.GetType() != tastoratypes.NodeTypeValidator {
			continue
		}

		nodeClient, err := n.GetRPCClient()
		if err != nil {
			return fmt.Errorf("failed to get RPC client for node %d: %w", i, err)
		}

		status, err := nodeClient.Status(ctx)
		if err != nil {
			return fmt.Errorf("failed to get status for node %d: %w", i, err)
		}

		// the +10 is just to leave room for error
		if (status.SyncInfo.LatestBlockHeight + 10) < endHeight {
			haltedNodes = append(haltedNodes, fmt.Sprintf("node_%d (height_%d)", i, status.SyncInfo.LatestBlockHeight))
		}
	}

	if len(haltedNodes) > 0 {
		return fmt.Errorf("%d validator node(s) halted below expected height %d: %v", len(haltedNodes), endHeight, haltedNodes)
	}

	return nil
}
