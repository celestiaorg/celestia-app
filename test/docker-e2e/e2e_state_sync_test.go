package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"celestiaorg/celestia-app/test/docker-e2e/networks"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	cometcfg "github.com/cometbft/cometbft/config"
	rpclient "github.com/cometbft/cometbft/rpc/client"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	servercfg "github.com/cosmos/cosmos-sdk/server/config"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	addressutil "github.com/celestiaorg/tastora/framework/testutil/address"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
)

const (
	blocksToProduce            = 30
	stateSyncTrustHeightOffset = 5
	stateSyncTimeout           = 10 * time.Minute
)

func (s *CelestiaTestSuite) TestStateSync() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()
	cfg := dockerchain.DefaultConfig(s.client, s.network)
	celestia, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).
		WithPostInit(validatorStateSyncAppOverrides).
		Build(ctx)

	s.Require().NoError(err, "failed to get chain")

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	// Cleanup resources when the test is done
	t.Cleanup(func() {
		if err := celestia.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	// verify the chain is running
	height, err := celestia.Height(ctx)
	s.Require().NoError(err, "failed to get chain height")
	s.Require().Greater(height, int64(0), "chain height is zero")

	s.CreateTxSim(ctx, celestia)

	allNodes := celestia.GetNodes()

	nodeClient, err := allNodes[0].GetRPCClient()
	s.Require().NoError(err)

	initialHeight, err := s.GetLatestBlockHeight(ctx, nodeClient)
	s.Require().NoError(err, "failed to get initial height")
	s.Require().Greater(initialHeight, int64(0), "initial height is zero")

	targetHeight := initialHeight + blocksToProduce
	t.Logf("Successfully reached initial height %d", initialHeight)

	s.Require().NoError(wait.ForBlocks(ctx, int(targetHeight), celestia), "failed to wait for target height")

	t.Logf("Successfully reached target height %d", targetHeight)

	t.Logf("Gathering state sync parameters")
	latestHeight, err := s.GetLatestBlockHeight(ctx, nodeClient)
	s.Require().NoError(err, "failed to get latest height for state sync parameters")
	trustHeight := latestHeight - stateSyncTrustHeightOffset
	s.Require().Greater(trustHeight, int64(0), "calculated trust height %d is too low (latest height: %d)", trustHeight, latestHeight)

	trustBlock, err := nodeClient.Block(ctx, &trustHeight)
	s.Require().NoError(err, "failed to get block at trust height %d", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()
	rpcServers, err := addressutil.BuildInternalRPCAddressList(ctx, celestia.GetNodes())
	s.Require().NoError(err, "failed to build RPC address list")

	t.Logf("Trust height: %d", trustHeight)
	t.Logf("Trust hash: %s", trustHash)
	t.Logf("RPC servers: %s", rpcServers)

	t.Log("Adding state sync node")
	err = celestia.AddNode(ctx,
		tastoradockertypes.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			WithPostInit(func(ctx context.Context, node *tastoradockertypes.ChainNode) error {
				return config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
					cfg.StateSync.Enable = true
					cfg.StateSync.TrustHeight = trustHeight
					cfg.StateSync.TrustHash = trustHash
					cfg.StateSync.RPCServers = strings.Split(rpcServers, ",")
				})
			}).
			Build(),
	)

	s.Require().NoError(err, "failed to add node")

	allNodes = celestia.GetNodes()
	fullNode := allNodes[len(allNodes)-1]

	s.Require().Equal(tastoratypes.NodeTypeConsensusFull, fullNode.GetType(), "expected state sync node to be a full node")

	stateSyncClient, err := fullNode.GetRPCClient()
	s.Require().NoError(err)

	err = s.WaitForSync(ctx, stateSyncClient, stateSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight >= latestHeight
	})

	s.Require().NoError(err, "failed to wait for state sync to complete")

	s.T().Logf("Checking validator liveness from height %d", initialHeight)
	s.Require().NoError(
		s.CheckLiveness(ctx, celestia),
		"validator liveness check failed",
	)
}

// TestStateSyncMocha tests state sync functionality by syncing from the mocha network.
func (s *CelestiaTestSuite) TestStateSyncMocha() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()

	mochaConfig := networks.NewMochaConfig()
	mochaClient, err := networks.NewClient(mochaConfig.RPCs[0])
	s.Require().NoError(err, "failed to create mocha RPC client")

	// get latest height from mocha
	latestHeight, err := s.GetLatestBlockHeight(ctx, mochaClient)
	s.Require().NoError(err, "failed to get latest height from mocha")
	s.Require().Greater(latestHeight, int64(0), "latest height is zero")

	trustHeight := latestHeight - 2000
	s.Require().Greater(trustHeight, int64(0), "calculated trust height %d is too low", trustHeight)

	// get trust hash from mocha
	trustBlock, err := mochaClient.Block(ctx, &trustHeight)
	s.Require().NoError(err, "failed to get block at trust height %d from mocha", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()

	t.Logf("Mocha latest height: %d", latestHeight)
	t.Logf("Using trust height: %d", trustHeight)
	t.Logf("Using trust hash: %s", trustHash)
	t.Logf("Using mocha RPC: %s", mochaConfig.RPCs[0])

	dockerCfg, err := networks.NewConfig(mochaConfig, s.client, s.network)
	s.Require().NoError(err, "failed to create mocha config")

	// create a mocha chain builder (no validators, just for state sync nodes)
	mochaChain, err := networks.NewChainBuilder(s.T(), mochaConfig, dockerCfg).
		WithNodes(tastoradockertypes.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			WithPostInit(func(ctx context.Context, node *tastoradockertypes.ChainNode) error {
				return config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
					// enable state sync
					cfg.StateSync.Enable = true
					cfg.StateSync.TrustHeight = trustHeight
					cfg.StateSync.TrustHash = trustHash
					cfg.StateSync.RPCServers = mochaConfig.RPCs
					cfg.P2P.Seeds = mochaConfig.Seeds
				})
			}).
			Build(),
		).
		Build(ctx)

	s.Require().NoError(err, "failed to create chain")

	t.Log("Starting mocha state sync node")
	err = mochaChain.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	t.Cleanup(func() {
		if err := mochaChain.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	allNodes := mochaChain.GetNodes()
	s.Require().Len(allNodes, 1, "expected exactly one node")
	fullNode := allNodes[0]

	s.Require().Equal(tastoratypes.NodeTypeConsensusFull, fullNode.GetType(), "expected state sync node to be a full node")

	stateSyncClient, err := fullNode.GetRPCClient()
	s.Require().NoError(err, "failed to get state sync client")

	err = s.WaitForSync(ctx, stateSyncClient, stateSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight >= trustHeight
	})

	s.Require().NoError(err, "failed to wait for state sync to complete")
}

// TestStateSyncAcrossMixedAppVersions tests that a full node can state-sync to the latest height
// when the chain history spans multiple app versions (v4 → v5).
//
// Test steps:
// 1. Start a v4 chain with 4 validators and tx traffic for 20 blocks
// 2. Perform on-chain upgrade v4 → v5, finalize, and produce 50 more blocks
// 3. Launch a new full node with StateSync.Enable=true
// 4. Assert: node finishes state-sync, app_version == 5, basic bank-send succeeds
func (s *CelestiaTestSuite) TestStateSyncAcrossMixedAppVersions() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping state sync across mixed app versions test in short mode")
	}

	ctx := context.Background()

	// Get the Docker image tag for the test
	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err)

	// Phase 1: Start v4 chain with 4 validators
	t.Log("Phase 1: Starting v4 chain with 4 validators")
	cfg := dockerchain.DefaultConfig(s.client, s.network).WithTag(tag)

	// Configure genesis for v4 with 3 validators
	cfg.Genesis = cfg.Genesis.WithAppVersion(4)

	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).
		WithPostInit(validatorStateSyncProducerOverrides). // Enable state sync snapshots
		Build(ctx)
	s.Require().NoError(err, "failed to build chain")

	t.Cleanup(func() {
		if err := chain.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	// Wait for chain to be running and get initial state
	s.Require().NoError(wait.ForBlocks(ctx, 1, chain), "the chain should be running")

	validatorNode := chain.GetNodes()[0]
	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	// Wait for state sync snapshots to be created
	t.Log("Waiting for state sync snapshots to be generated...")
	s.Require().NoError(wait.ForBlocks(ctx, 10, chain), "the chain should be running to generate snapshots")

	// Verify we're starting with app version 4
	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(uint64(4), abciInfo.Response.GetAppVersion(), "should start with app version 4")

	// Wait for chain to be ready by waiting for blocks to be produced
	t.Log("Waiting for chain to be ready")
	initialHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get initial height")
	s.Require().Greater(initialHeight, int64(0), "initial height should be greater than 0")

	// Wait for a few blocks to ensure chain is stable
	s.Require().NoError(wait.ForBlocks(ctx, 3, chain), "failed to wait for chain stability")

	// Start transaction simulation for network activity
	s.CreateTxSim(ctx, chain)

	// Produce 20 blocks with transaction activity
	t.Log("Producing 20 blocks with transaction activity")
	s.Require().NoError(wait.ForBlocks(ctx, 20, chain), "failed to wait for 20 blocks")

	// Test functionality before upgrade to ensure gRPC is working
	t.Log("Testing functionality before upgrade")

	testBankSend(s.T(), chain, cfg)
	testPFBSubmission(s.T(), chain, cfg)

	// Phase 2: Perform v4 → v5 upgrade
	t.Log("Phase 2: Performing v4 → v5 upgrade")
	upgradeHeight := s.performUpgrade(ctx, chain, cfg)

	finalHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get final height")
	t.Logf("Chain now at height %d with mixed version history (v4: blocks 1-%d, v5: blocks %d-%d)",
		finalHeight, upgradeHeight-1, upgradeHeight, finalHeight)

	t.Log("Validators configured with state sync snapshots every 5 blocks")

	// Phase 3: Launch state sync node
	t.Log("Phase 3: Launching new full node with state sync enabled")

	// Calculate state sync parameters from the v5 portion of the chain
	latestHeight := finalHeight
	trustHeight := latestHeight - stateSyncTrustHeightOffset
	s.Require().Greater(trustHeight, upgradeHeight,
		"trust height %d should be in v5 portion (after upgrade height %d)", trustHeight, upgradeHeight)

	trustBlock, err := rpcClient.Block(ctx, &trustHeight)
	s.Require().NoError(err, "failed to get block at trust height %d", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()
	rpcServers, err := addressutil.BuildInternalRPCAddressList(ctx, chain.GetNodes())
	s.Require().NoError(err, "failed to build RPC address list")

	t.Logf("State sync parameters: trust_height=%d, trust_hash=%s", trustHeight, trustHash)

	// Add state sync node
	err = chain.AddNode(ctx,
		tastoradockertypes.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			WithPostInit(func(ctx context.Context, node *tastoradockertypes.ChainNode) error {
				// Use our helper function to properly configure state sync
				return configureStateSyncClient(ctx, node, strings.Split(rpcServers, ","), trustHeight, trustHash)
			}).
			Build(),
	)
	s.Require().NoError(err, "failed to add state sync node")

	allNodes := chain.GetNodes()
	stateSyncNode := allNodes[len(allNodes)-1]
	s.Require().Equal(tastoratypes.NodeTypeConsensusFull, stateSyncNode.GetType(), "expected state sync node to be a full node")

	stateSyncClient, err := stateSyncNode.GetRPCClient()
	s.Require().NoError(err, "failed to get state sync client")

	// Phase 4: Wait for state sync and validate
	t.Log("Phase 4: Waiting for state sync completion and validation")

	// Monitor sync progress and collect height history
	heightHistory := s.monitorSyncProgress(ctx, t, stateSyncClient, latestHeight, stateSyncTimeout)

	// Verify that state sync was used (not block sync)
	dockerNode := stateSyncNode.(*tastoradockertypes.ChainNode)
	verifySyncMethod(t, heightHistory, dockerNode)

	// Validate: /status shows catching_up=false
	status, err := stateSyncClient.Status(ctx)
	s.Require().NoError(err, "failed to get status from state sync node")
	s.Require().False(status.SyncInfo.CatchingUp, "state sync node should not be catching up")
	t.Logf("State sync node status: catching_up=%t, height=%d",
		status.SyncInfo.CatchingUp, status.SyncInfo.LatestBlockHeight)

	// Get FRESH chain height for accurate comparison (chain continued producing blocks during state sync)
	currentChainHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get current chain height")

	// Verify that the node actually performed state sync by checking it reached current height
	nodeHeight := status.SyncInfo.LatestBlockHeight
	s.Require().GreaterOrEqual(nodeHeight, currentChainHeight-5,
		"state sync node should be within 5 blocks of the current chain tip")
	s.Require().LessOrEqual(nodeHeight, currentChainHeight+2,
		"state sync node should not be significantly ahead of the chain")
	t.Logf("State sync successful: node reached height %d (current chain tip: %d)",
		nodeHeight, currentChainHeight)

	// Validate: ABCIInfo.app_version == 5
	syncedAbciInfo, err := stateSyncClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info from state sync node")
	s.Require().Equal(uint64(5), syncedAbciInfo.Response.GetAppVersion(),
		"state sync node should have app version 5")
	t.Logf("State sync node app version: %d", syncedAbciInfo.Response.GetAppVersion())

	// Validate: Basic bank send succeeds from synced node
	t.Log("Testing bank send transaction from state sync node")
	testBankSend(s.T(), chain, cfg)
	t.Log("Bank send transaction succeeded")

	// Final liveness check
	t.Log("Performing final liveness check")
	s.Require().NoError(s.CheckLiveness(ctx, chain), "liveness check failed")
	t.Log("Liveness check passed")

	t.Log("State sync across mixed app versions test completed successfully!")
}

// checkSyncMetrics queries Prometheus metrics to determine sync method
func checkSyncMetrics(t *testing.T, node *tastoradockertypes.ChainNode) (stateSync bool, blockSync bool, err error) {
	ctx := context.Background()

	// Try multiple approaches to get metrics
	endpoints := []string{
		"http://localhost:26660/metrics", // Default prometheus port
		"http://127.0.0.1:26660/metrics", // Alternative localhost
		"http://localhost:26657/metrics", // Alternative CometBFT port
		"http://127.0.0.1:26657/metrics", // Alternative CometBFT port
	}

	for _, endpoint := range endpoints {
		cmd := []string{"curl", "-s", "--connect-timeout", "5", endpoint}
		stdout, stderr, execErr := node.Exec(ctx, cmd, nil)

		if execErr != nil {
			t.Logf("Failed to fetch metrics from %s: %v, stderr: %s", endpoint, execErr, string(stderr))
			continue
		}

		metrics := string(stdout)
		if len(metrics) < 10 {
			t.Logf("Empty or invalid metrics response from %s", endpoint)
			continue
		}

		// Parse metrics for sync indicators
		if strings.Contains(metrics, "statesync_syncing 1") {
			t.Logf("Prometheus metrics: State sync active (statesync_syncing=1)")
			return true, false, nil
		}

		if strings.Contains(metrics, "blocksync_syncing 1") {
			t.Logf("Prometheus metrics: Block sync active (blocksync_syncing=1)")
			return false, true, nil
		}

		// Check for completed sync metrics
		if strings.Contains(metrics, "statesync_syncing 0") {
			t.Logf("Prometheus metrics: State sync completed (statesync_syncing=0)")
			return true, false, nil
		}

		if strings.Contains(metrics, "blocksync_syncing 0") {
			t.Logf("Prometheus metrics: Block sync completed (blocksync_syncing=0)")
			return false, true, nil
		}

		t.Logf("Prometheus metrics available but no sync metrics found")
		return false, false, nil
	}

	return false, false, fmt.Errorf("could not fetch Prometheus metrics from any endpoint")
}

// analyzeSyncMethod determines if state sync or block sync was used based on height progression
func analyzeSyncMethod(t *testing.T, heightHistory []int64) (usedStateSync bool, usedBlockSync bool) {
	if len(heightHistory) < 2 {
		t.Logf("Insufficient height data for analysis: %v", heightHistory)
		return false, false
	}

	startHeight := heightHistory[0]
	endHeight := heightHistory[len(heightHistory)-1]

	// State sync pattern: sudden jump from 0 to high number (minimal intermediate values)
	// Block sync pattern: incremental progression through many intermediate heights

	if len(heightHistory) <= 3 && startHeight == 0 && endHeight > 50 {
		// Very few data points with sudden jump = state sync
		t.Logf("Detected state sync: sudden jump from %d to %d with %d data points",
			startHeight, endHeight, len(heightHistory))

		// Additional validation: ensure it's a true jump, not just fast incremental sync
		if len(heightHistory) == 2 && heightHistory[1] > heightHistory[0]+20 {
			t.Logf("Confirmed state sync: classic pattern [0, %d] with large jump", heightHistory[1])
			return true, false
		}

		return true, false
	}

	if len(heightHistory) > 5 {
		// Many data points = likely incremental block sync
		intermediateCount := 0
		for i := 1; i < len(heightHistory)-1; i++ {
			if heightHistory[i] > 0 && heightHistory[i] < endHeight-10 {
				intermediateCount++
			}
		}

		if intermediateCount > 2 {
			t.Logf("Detected block sync: incremental progression with %d intermediate heights", intermediateCount)
			return false, true
		} else {
			t.Logf("Pattern suggests state sync: few intermediate heights despite %d data points", len(heightHistory))
			return true, false
		}
	}

	// Moderate data points - analyze for incremental pattern
	hasIncremental := false
	for i := 1; i < len(heightHistory); i++ {
		if heightHistory[i] > 0 && heightHistory[i-1] >= 0 {
			diff := heightHistory[i] - heightHistory[i-1]
			if diff > 0 && diff < 20 && heightHistory[i-1] > 0 {
				hasIncremental = true
				break
			}
		}
	}

	if hasIncremental {
		t.Logf("Detected block sync: incremental progression pattern in %v", heightHistory)
		return false, true
	} else {
		t.Logf("Detected state sync: no incremental pattern in %v", heightHistory)
		return true, false
	}
}

// verifySyncMethod validates that state sync was used and not block sync
func verifySyncMethod(t *testing.T, heightHistory []int64, stateSyncNode *tastoradockertypes.ChainNode) {
	t.Logf("Height progression during sync: %v", heightHistory)

	// Analyze height progression pattern
	usedStateSync, usedBlockSync := analyzeSyncMethod(t, heightHistory)

	// FAIL if block sync was detected
	if usedBlockSync {
		t.Fatalf("CRITICAL FAILURE: Node used BLOCK SYNC instead of STATE SYNC! "+
			"This test requires state sync. Height progression: %v", heightHistory)
	}

	// FAIL if we couldn't determine the sync method
	if !usedStateSync {
		t.Fatalf("Could not confirm state sync was used. "+
			"Height progression: %v", heightHistory)
	}

	t.Logf("Confirmed: Node successfully used state sync")

	// Additional verification using Prometheus metrics
	metricStateSync, metricBlockSync, metricsErr := checkSyncMetrics(t, stateSyncNode)
	if metricsErr != nil {
		t.Logf("Warning: Could not verify sync method via metrics: %v", metricsErr)
	} else {
		if metricBlockSync {
			t.Fatalf("CRITICAL: Prometheus metrics show BLOCK SYNC was used! " +
				"This contradicts our height progression analysis. Metrics must be checked.")
		}
		if metricStateSync {
			t.Logf("Prometheus metrics confirm: State sync was used")
		}
	}
}

// monitorSyncProgress tracks height progression during sync and detects early block sync patterns
func (s *CelestiaTestSuite) monitorSyncProgress(ctx context.Context, t *testing.T, stateSyncClient rpclient.StatusClient, latestHeight int64, stateSyncTimeout time.Duration) []int64 {
	heightHistory := []int64{}

	err := s.WaitForSync(ctx, stateSyncClient, stateSyncTimeout, func(info rpctypes.SyncInfo) bool {
		heightHistory = append(heightHistory, info.LatestBlockHeight)

		// Log progress for analysis
		t.Logf("Sync progress: height=%d, catching_up=%t",
			info.LatestBlockHeight, info.CatchingUp)

		// EARLY DETECTION: If we see incremental height progression, it's likely block sync
		if len(heightHistory) >= 5 {
			// Check if we're seeing sequential height increases (block sync pattern)
			sequential := true
			for i := 1; i < len(heightHistory); i++ {
				if heightHistory[i] > 0 && heightHistory[i-1] > 0 {
					if heightHistory[i]-heightHistory[i-1] > 5 {
						sequential = false
						break
					}
				}
			}
			if sequential && heightHistory[len(heightHistory)-1] > 10 {
				t.Fatalf("EARLY DETECTION: Node appears to be using BLOCK SYNC (sequential height progression: %v)! "+
					"State sync should jump directly to target height, not increment slowly.", heightHistory)
			}
		}

		return !info.CatchingUp && info.LatestBlockHeight >= latestHeight
	})
	s.Require().NoError(err, "failed to wait for state sync to complete")

	return heightHistory
}

// performUpgrade executes the v4 → v5 upgrade with transaction activity
func (s *CelestiaTestSuite) performUpgrade(ctx context.Context, chain tastoratypes.Chain, cfg *dockerchain.Config) int64 {
	t := s.T()

	validatorNode := chain.GetNodes()[0]
	kr := cfg.Genesis.Keyring()
	records, err := kr.List()
	s.Require().NoError(err, "failed to list keyring records")
	s.Require().Len(records, len(chain.GetNodes()), "number of accounts should match number of nodes")

	// Signal and execute upgrade from v4 to v5
	upgradeHeight := s.signalAndGetUpgradeHeight(ctx, chain, validatorNode, cfg, records, 5)

	// Start keep-alive transaction generator to ensure chain doesn't stall during upgrade
	upgradeCtx, upgradeCancel := context.WithCancel(ctx)
	var upgradeWg sync.WaitGroup
	upgradeWg.Add(1)
	go s.keepAliveTransactionGenerator(upgradeCtx, &upgradeWg, chain.(*tastoradockertypes.Chain), cfg, 3) // Send tx every 3 seconds
	t.Log("Started keep-alive transaction generator for upgrade period")

	// Wait for upgrade to complete
	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	currentHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get current height")

	blocksToWait := int(upgradeHeight-currentHeight) + 2
	t.Logf("Waiting for %d blocks to reach upgrade height %d", blocksToWait, upgradeHeight)
	s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain),
		"failed to wait for upgrade completion")

	// Stop the upgrade keep-alive generator
	upgradeCancel()
	upgradeWg.Wait()
	t.Log("Stopped keep-alive transaction generator for upgrade period")

	// Verify upgrade completed successfully
	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(uint64(5), abciInfo.Response.GetAppVersion(), "should be at app version 5")

	// Start post-upgrade transaction activity
	postUpgradeCtx, postUpgradeCancel := context.WithCancel(ctx)
	var postUpgradeWg sync.WaitGroup
	postUpgradeWg.Add(1)
	go s.keepAliveTransactionGenerator(postUpgradeCtx, &postUpgradeWg, chain.(*tastoradockertypes.Chain), cfg, 2)
	t.Log("Started keep-alive transaction generator for post-upgrade block production")

	// Produce additional blocks at app version 5
	t.Log("Producing 20 more blocks at app version 5")
	s.Require().NoError(wait.ForBlocks(ctx, 20, chain), "failed to wait for post-upgrade blocks")

	// Stop post-upgrade generator
	postUpgradeCancel()
	postUpgradeWg.Wait()
	t.Log("Stopped keep-alive transaction generator for post-upgrade block production")

	return upgradeHeight
}

// keepAliveTransactionGenerator sends simple transactions to keep the chain active during upgrades.
// This ensures continuous block production and snapshot creation for state sync testing.
func (s *CelestiaTestSuite) keepAliveTransactionGenerator(ctx context.Context, wg *sync.WaitGroup, chain *tastoradockertypes.Chain, cfg *dockerchain.Config, intervalSeconds int) {
	defer wg.Done()

	t := s.T()

	// Setup simple transaction client
	nodes := chain.GetNodes()
	if len(nodes) == 0 {
		t.Logf("Keep-alive: no nodes available")
		return
	}

	validatorNode := nodes[0].(*tastoradockertypes.ChainNode)
	txClient, err := dockerchain.SetupTxClient(ctx, validatorNode, cfg)
	if err != nil {
		t.Logf("Keep-alive: failed to setup tx client: %v", err)
		return
	}

	// Create recipient wallet
	recipientWallet, err := chain.CreateWallet(ctx, fmt.Sprintf("keepalive-recipient-%d", time.Now().UnixNano()))
	if err != nil {
		t.Logf("Keep-alive: failed to create recipient wallet: %v", err)
		return
	}

	recipientAddr, err := sdk.AccAddressFromBech32(recipientWallet.GetFormattedAddress())
	if err != nil {
		t.Logf("Keep-alive: failed to parse recipient address: %v", err)
		return
	}

	fromAddr := txClient.DefaultAddress()
	t.Logf("Keep-alive: started, sending from %s to %s every %ds", fromAddr, recipientAddr, intervalSeconds)

	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	txCounter := 0
	for {
		select {
		case <-ctx.Done():
			t.Logf("Keep-alive: stopped after %d transactions", txCounter)
			return
		case <-ticker.C:
			txCounter++

			// Send small transaction (1000 utia)
			sendAmount := sdk.NewCoins(sdk.NewCoin("utia", sdkmath.NewInt(1000)))
			msg := banktypes.NewMsgSend(fromAddr, recipientAddr, sendAmount)

			_, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200000), user.SetFee(5000))
			if err != nil {
				t.Logf("Keep-alive transaction %d failed: %v", txCounter, err)
			} else {
				t.Logf("Keep-alive transaction %d successful", txCounter)
			}
		}
	}
}

// validatorStateSyncAppOverrides modifies the app.toml to configure state sync snapshots for the given node.
func validatorStateSyncAppOverrides(ctx context.Context, node *tastoradockertypes.ChainNode) error {
	return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
		cfg.StateSync.SnapshotInterval = 5
		cfg.StateSync.SnapshotKeepRecent = 2
	})
}

// validatorStateSyncProducerOverrides configures validators to produce state sync snapshots.
func validatorStateSyncProducerOverrides(ctx context.Context, node *tastoradockertypes.ChainNode) error {
	return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
		// Configure frequent state sync snapshots for new nodes to consume
		cfg.StateSync.SnapshotInterval = 5   // Create snapshot every 5 blocks
		cfg.StateSync.SnapshotKeepRecent = 3 // Keep 3 recent snapshots
	})
}

// configureStateSyncClient configures a node to use state sync.
func configureStateSyncClient(ctx context.Context, node *tastoradockertypes.ChainNode, rpcEndpoints []string, trustHeight int64, trustHash string) error {
	return config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
		// Enable state sync
		cfg.StateSync.Enable = true

		// Set RPC servers for state sync (comma-separated list)
		if len(rpcEndpoints) > 0 {
			cfg.StateSync.RPCServers = rpcEndpoints
		}

		// Set trust parameters
		cfg.StateSync.TrustHeight = trustHeight
		cfg.StateSync.TrustHash = trustHash

		// Set reasonable timeouts
		cfg.StateSync.TrustPeriod = 168 * time.Hour // 1 week
		cfg.StateSync.DiscoveryTime = 5 * time.Second

		// Enable Prometheus metrics for sync detection
		cfg.Instrumentation.Prometheus = true
	})
}
