package docker_e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

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
	defaultBlocksPerValidator = 3                    // Minimum blocks each validator should propose for liveness validation
	validatorAddrTruncateLen  = 8                    // Length to truncate validator addresses for display
	validatorNameFormat       = "validator_%s"       // Format string for validator display names
	nodeStatusFormat          = "node_%d(height_%d)" // Format string for node status messages

	// Log message formats
	waitingForBlocksLogFormat    = "Waiting for %d more blocks to meet minimum requirement (%d produced, %d required for %d validators × %d blocks each)"
	blockRequirementMetLogFormat = "Minimum block requirement already met: %d blocks ≥ %d required (%d validators × %d blocks each)"

	homeDir = "/var/cosmos-chain/celestia"
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
func (s *CelestiaTestSuite) CheckLiveness(
	ctx context.Context,
	chain tastoratypes.Chain,
	startHeight int64,
) error {
	rpcClient, err := chain.GetNodes()[0].GetRPCClient()
	if err != nil {
		return fmt.Errorf("failed to get RPC client: %w", err)
	}

	endHeight, err := s.ensureMinimumBlocks(ctx, chain, rpcClient, startHeight)
	if err != nil {
		return fmt.Errorf("failed to ensure minimum blocks: %w", err)
	}

	startValidators, endValidators, err := s.fetchValidatorSets(ctx, rpcClient, startHeight, endHeight)
	if err != nil {
		return fmt.Errorf("failed to fetch validator sets: %w", err)
	}

	proposers, err := s.fetchProposerAddresses(ctx, rpcClient, startHeight, endHeight)
	if err != nil {
		return fmt.Errorf("failed to fetch proposer addresses: %w", err)
	}

	if err := s.validateAllValidatorsProposed(startValidators, endValidators, proposers, startHeight, endHeight); err != nil {
		return err
	}

	if err := s.validateNodesNotHalted(ctx, chain, endHeight); err != nil {
		return err
	}

	s.T().Logf("✅ Liveness check passed: all validators proposed blocks and all nodes reached height %d", endHeight)
	return nil
}

// ensureMinimumBlocks waits for additional blocks if necessary to meet the minimum
// requirement of defaultBlocksPerValidator (3) blocks per validator since startHeight.
func (s *CelestiaTestSuite) ensureMinimumBlocks(
	ctx context.Context,
	chain tastoratypes.Chain,
	rpcClient rpcclient.Client,
	startHeight int64,
) (int64, error) {
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
		s.T().Logf(blockRequirementMetLogFormat,
			blocksProduced, minBlocksRequired, numValidators, defaultBlocksPerValidator)
		return currentHeight, nil
	}

	additionalBlocksNeeded := minBlocksRequired - blocksProduced
	s.T().Logf(waitingForBlocksLogFormat,
		additionalBlocksNeeded, blocksProduced, minBlocksRequired, numValidators, defaultBlocksPerValidator)

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
func (s *CelestiaTestSuite) fetchValidatorSets(
	ctx context.Context,
	rpcClient rpcclient.Client,
	startHeight, endHeight int64,
) (*coretypes.ResultValidators, *coretypes.ResultValidators, error) {
	if endHeight <= startHeight {
		return nil, nil, fmt.Errorf("invalid height range %d to %d", startHeight, endHeight)
	}

	blocksProduced := endHeight - startHeight
	s.T().Logf("Checking validator liveness from height %d to %d (%d blocks)", startHeight, endHeight, blocksProduced)

	startValidators, err := rpcClient.Validators(ctx, &startHeight, nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("validators query at start height %d: %w", startHeight, err)
	}

	endValidators, err := rpcClient.Validators(ctx, &endHeight, nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("validators query at end height %d: %w", endHeight, err)
	}

	return startValidators, endValidators, nil
}

// fetchProposerAddresses gathers proposer addresses from block headers using efficient batching
func (s *CelestiaTestSuite) fetchProposerAddresses(
	ctx context.Context,
	rpcClient rpcclient.Client,
	startHeight, endHeight int64,
) ([]string, error) {
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
			return nil, fmt.Errorf("failed to fetch block headers for range %d-%d: %w", minHeight, maxHeight, err)
		}

		if len(blockchainInfo.BlockMetas) == 0 {
			return nil, fmt.Errorf("no block headers returned for range %d-%d", minHeight, maxHeight)
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
func (s *CelestiaTestSuite) validateAllValidatorsProposed(
	startValidators, endValidators *coretypes.ResultValidators,
	proposerAddresses []string,
	startHeight, endHeight int64,
) error {
	// Convert slice to map for O(1) lookups
	proposers := make(map[string]struct{}, len(proposerAddresses))
	for _, addr := range proposerAddresses {
		proposers[addr] = struct{}{}
	}

	// Create a combined map of all validators that should have proposed
	var (
		allValidators  = make(map[string]struct{})
		validatorNames = make(map[string]string) // for better error reporting
	)

	// Add start validators
	for _, val := range startValidators.Validators {
		addr := val.Address.String()
		allValidators[addr] = struct{}{}
		validatorNames[addr] = fmt.Sprintf(validatorNameFormat, addr[:validatorAddrTruncateLen]) // shortened for readability
	}

	// Add end validators (in case validator set changed)
	for _, val := range endValidators.Validators {
		addr := val.Address.String()
		allValidators[addr] = struct{}{}
		validatorNames[addr] = fmt.Sprintf(validatorNameFormat, addr[:validatorAddrTruncateLen])
	}

	s.T().Logf("Checking %d total validators for proposer activity from height %d to %d (validators at start: %d, validators at end: %d)",
		len(allValidators), startHeight, endHeight, len(startValidators.Validators), len(endValidators.Validators))

	// Verify every validator appears in proposers
	var missingValidators []string
	for validatorAddr := range allValidators {
		if _, ok := proposers[validatorAddr]; !ok {
			missingValidators = append(missingValidators, validatorNames[validatorAddr])
		}
	}

	if len(missingValidators) > 0 {
		return fmt.Errorf("%d validator(s) never proposed blocks from height %d to %d: %v",
			len(missingValidators), startHeight, endHeight, missingValidators)
	}

	return nil
}

// validateNodesNotHalted ensures no nodes halted below the expected height
func (s *CelestiaTestSuite) validateNodesNotHalted(
	ctx context.Context,
	chain tastoratypes.Chain,
	endHeight int64,
) error {
	var haltedNodes []string
	for i, n := range chain.GetNodes() {
		nodeClient, err := n.GetRPCClient()
		if err != nil {
			return fmt.Errorf("failed to get RPC client for node %d: %w", i, err)
		}
		status, err := nodeClient.Status(ctx)
		if err != nil {
			return fmt.Errorf("failed to get status for node %d: %w", i, err)
		}
		if status.SyncInfo.LatestBlockHeight < endHeight {
			haltedNodes = append(haltedNodes, fmt.Sprintf(nodeStatusFormat, i, status.SyncInfo.LatestBlockHeight))
		}
	}

	if len(haltedNodes) > 0 {
		return fmt.Errorf("%d node(s) halted below expected height %d: %v",
			len(haltedNodes), endHeight, haltedNodes)
	}

	return nil
}
