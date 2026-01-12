package docker_e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	addressutil "github.com/celestiaorg/tastora/framework/testutil/address"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	rpcclient "github.com/cometbft/cometbft/rpc/client"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
)

// TestStateSyncWithAppUpgrade verifies that a full node can state-sync across app version
// boundaries. The test creates a snapshot from before an app upgrade and consumes it with
// a node running the new app version, ensuring state sync works across version changes.
func (s *CelestiaTestSuite) TestStateSyncWithAppUpgrade() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping state sync with app upgrade test in short mode")
	}

	ctx := context.Background()

	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err)

	cfg := dockerchain.DefaultConfig(s.client, s.network).WithTag(tag)
	t.Logf("Phase 1: Starting the chain with %d validators", len(cfg.Genesis.Validators()))

	const baseAppVersion = appconsts.Version - 1
	const targetAppVersion = appconsts.Version
	t.Logf("Starting chain with app version %d and will upgrade to %d.", baseAppVersion, targetAppVersion)
	cfg.Genesis = cfg.Genesis.WithAppVersion(baseAppVersion)

	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).WithPostInit(validatorStateSyncProducerOverrides).Build(ctx)
	s.Require().NoError(err, "failed to build chain")

	t.Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	validatorNode := chain.GetNodes()[0]
	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	t.Log("Waiting for state sync snapshots to be generated...")
	s.Require().NoError(wait.ForBlocks(ctx, 10, chain), "the chain should be running to generate snapshots")

	// Verify we're starting with the right app version
	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(baseAppVersion, abciInfo.Response.GetAppVersion(), "should start with app version %v", baseAppVersion)

	s.CreateTxSim(ctx, chain)

	t.Log("Producing 20 blocks with transaction activity")
	s.Require().NoError(wait.ForBlocks(ctx, 20, chain), "failed to wait for 20 blocks")

	t.Log("Testing functionality before upgrade")
	testBankSend(s.T(), chain, cfg)
	testPFBSubmission(s.T(), chain, cfg)

	t.Log("Phase 2: Waiting for snapshot creation before upgrade")
	// Wait for a snapshot to be created at the base app version
	// This ensures we have a snapshot from the old version to test cross-version state sync
	snapshotHeight := s.waitForSnapshotCreation(ctx, chain, rpcClient)
	t.Logf("Snapshot created at height %d (app version %d)", snapshotHeight, baseAppVersion)

	t.Log("Phase 3: Performing upgrade")
	upgradeHeight := s.performUpgrade(ctx, chain, cfg, targetAppVersion)

	finalHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get final height")
	t.Logf("Blocks 1 to %d have app version %d", upgradeHeight-1, baseAppVersion)
	t.Logf("Blocks %d to %d have app version %d", upgradeHeight, finalHeight, targetAppVersion)

	t.Log("Phase 4: Launching new full node with state sync enabled")

	// Key difference from basic state sync test: Use snapshot from BEFORE the upgrade
	// This tests state sync across app version boundaries (snapshot producer: old version, consumer: new version)
	// vs basic state sync test where both producer and consumer are on the same version
	trustHeight := snapshotHeight
	s.Require().Greater(trustHeight, int64(0), "trust height should be positive")
	s.Require().Less(trustHeight, upgradeHeight, "trust height %d should be before upgrade height %d to test cross-version state sync", trustHeight, upgradeHeight)

	trustBlock, err := rpcClient.Block(ctx, &trustHeight)
	s.Require().NoError(err, "failed to get block at trust height %d", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()
	rpcServers, err := addressutil.BuildInternalRPCAddressList(ctx, chain.GetNodes())
	s.Require().NoError(err, "failed to build RPC address list")

	t.Logf("State sync parameters: trust_height=%d, trust_hash=%s", trustHeight, trustHash)

	// Add state sync node
	err = chain.AddNode(ctx,
		cosmos.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			WithPostInit(func(ctx context.Context, node *cosmos.ChainNode) error {
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

	t.Log("Waiting for node to state sync...")
	err = wait.ForCondition(ctx, stateSyncTimeout, 1*time.Second, func() (bool, error) {
		status, err := stateSyncClient.Status(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to get node status: %w", err)
		}
		return !status.SyncInfo.CatchingUp, nil
	})
	s.Require().NoError(err, "state sync node failed to complete sync within timeout")

	// Verify that state sync was used (not block sync) via metrics
	dockerNode := stateSyncNode.(*cosmos.ChainNode)
	verifyStateSync(t, dockerNode)

	// Validate: /status shows catching_up=false
	status, err := stateSyncClient.Status(ctx)
	s.Require().NoError(err, "failed to get status from state sync node")
	s.Require().False(status.SyncInfo.CatchingUp, "state sync node should not be catching up")
	t.Logf("State sync node status: catching_up=%t, height=%d", status.SyncInfo.CatchingUp, status.SyncInfo.LatestBlockHeight)

	// Get fresh chain height for accurate comparison (chain continued producing blocks during state sync)
	currentChainHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get current chain height")

	// Verify that the node actually performed state sync by checking it reached current height
	nodeHeight := status.SyncInfo.LatestBlockHeight
	s.Require().GreaterOrEqual(nodeHeight, currentChainHeight-5, "state sync node should be within 5 blocks of the current chain tip")
	s.Require().LessOrEqual(nodeHeight, currentChainHeight,
		"state sync node should not be ahead of chain tip")
	t.Logf("State sync successful: node reached height %d (current chain tip: %d)", nodeHeight, currentChainHeight)

	// Verify ABCIInfo.app_version == targetAppVersion
	syncedAbciInfo, err := stateSyncClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info from state sync node")
	s.Require().Equal(targetAppVersion, syncedAbciInfo.Response.GetAppVersion(), "state sync node should have app version %d", targetAppVersion)
	t.Logf("State sync node app version: %d", syncedAbciInfo.Response.GetAppVersion())
	binaryVersion := syncedAbciInfo.Response.GetVersion()

	// Verify cross-version state sync success
	t.Logf("Success: Cross-version state sync completed - consumed snapshot from app version %d with node running binary version %s", baseAppVersion, binaryVersion)

	// Final liveness check
	t.Log("Performing final liveness check")
	s.Require().NoError(s.CheckLiveness(ctx, chain), "liveness check failed")
	t.Log("Liveness check passed")
}

// performUpgrade executes the upgrade to the target app version
func (s *CelestiaTestSuite) performUpgrade(ctx context.Context, chain tastoratypes.Chain, cfg *dockerchain.Config, appVersion uint64) (upgradeHeight int64) {
	t := s.T()

	validatorNode := chain.GetNodes()[0]
	kr := cfg.Genesis.Keyring()
	records, err := kr.List()
	s.Require().NoError(err, "failed to list keyring records")
	s.Require().Len(records, len(chain.GetNodes()), "number of accounts should match number of nodes")

	upgradeHeight = s.signalAndGetUpgradeHeight(ctx, chain, validatorNode, cfg, records, appVersion)

	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	currentHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get current height")

	blocksToWait := int(upgradeHeight-currentHeight) + 2
	t.Logf("Waiting for %d blocks to reach upgrade height %d", blocksToWait, upgradeHeight)
	s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain), "failed to wait for upgrade completion")

	// Verify upgrade completed successfully
	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(appVersion, abciInfo.Response.GetAppVersion(), "should be at app version %v", appVersion)

	// Produce additional blocks at the target app version (TxSim is still running)
	t.Logf("Producing 20 more blocks at app version %v", appVersion)
	s.Require().NoError(wait.ForBlocks(ctx, 20, chain), "failed to wait for post-upgrade blocks")

	return upgradeHeight
}

// waitForSnapshotCreation waits for a snapshot to be created and returns its height
func (s *CelestiaTestSuite) waitForSnapshotCreation(ctx context.Context, chain tastoratypes.Chain, rpcClient rpcclient.Client) int64 {
	t := s.T()

	// Get current height
	currentHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get current height")

	t.Logf("Waiting for snapshot creation starting from height %d", currentHeight)

	// Wait for a snapshot to be created (snapshots are created every 5 blocks)
	// We need to wait for at least one snapshot interval to pass
	s.Require().NoError(wait.ForBlocks(ctx, 5, chain), "failed to wait for snapshot creation")

	// Get the height after waiting (this should be a snapshot height)
	snapshotHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get snapshot height")

	t.Logf("Snapshot creation completed at height %d", snapshotHeight)
	return snapshotHeight
}
