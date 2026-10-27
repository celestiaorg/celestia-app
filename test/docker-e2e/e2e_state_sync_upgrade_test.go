package docker_e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	addressutil "github.com/celestiaorg/tastora/framework/testutil/address"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
)

// TestStateSyncWithAppUpgrade verifies that a node running the new binary can
// state sync from a snapshot taken at the old app version and then follow the
// chain through the app upgrade.
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

	// Verify we're starting with the right app version
	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(baseAppVersion, abciInfo.Response.GetAppVersion(), "should start with app version %v", baseAppVersion)

	s.CreateTxSim(ctx, chain)

	t.Log("Producing 5 blocks with transaction activity")
	s.Require().NoError(wait.ForBlocks(ctx, 5, chain), "failed to wait for 5 blocks")

	t.Log("Testing functionality before upgrade")
	testBankSend(s.T(), chain, cfg)
	testPFBSubmission(s.T(), chain, cfg)

	t.Log("Phase 2: Waiting for snapshot creation before upgrade")
	trustHeight := s.waitForSnapshotCreation(ctx, chain, rpcClient)
	t.Logf("Snapshot created by height %d (app version %d)", trustHeight, baseAppVersion)

	// Sync the node before signaling the upgrade so every snapshot it can pick
	// was taken at the base app version. CometBFT restores the highest one.
	t.Log("Phase 3: Launching new full node with state sync enabled")
	trustBlock, err := rpcClient.Block(ctx, &trustHeight)
	s.Require().NoError(err, "failed to get block at trust height %d", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()
	rpcServers, err := addressutil.BuildInternalRPCAddressList(ctx, chain.GetNodes())
	s.Require().NoError(err, "failed to build RPC address list")

	t.Logf("State sync parameters: trust_height=%d, trust_hash=%s", trustHeight, trustHash)

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
	err = s.WaitForSync(ctx, stateSyncClient, stateSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight > 0
	})
	s.Require().NoError(err, "state sync node failed to complete sync within timeout")

	// Verify that state sync was used (not block sync) via metrics
	verifyStateSync(t, stateSyncNode.(*cosmos.ChainNode))

	// The block store of a state-synced node starts right after the snapshot.
	status, err := stateSyncClient.Status(ctx)
	s.Require().NoError(err, "failed to get status from state sync node")
	restoredHeight := status.SyncInfo.EarliestBlockHeight - 1
	restoredBlock, err := rpcClient.Block(ctx, &restoredHeight)
	s.Require().NoError(err, "failed to get block at restored snapshot height %d", restoredHeight)
	s.Require().Equal(baseAppVersion, restoredBlock.Block.Version.App, "restored snapshot at height %d should be from app version %d", restoredHeight, baseAppVersion)
	t.Logf("State sync node restored the snapshot at height %d (app version %d)", restoredHeight, baseAppVersion)

	t.Log("Phase 4: Performing upgrade")
	upgradeHeight := s.UpgradeChain(ctx, chain, cfg, targetAppVersion)
	s.Require().Less(restoredHeight, upgradeHeight, "restored snapshot height should be before the upgrade height")

	t.Log("Waiting for the state-synced node to follow the chain through the upgrade...")
	err = s.WaitForSync(ctx, stateSyncClient, stateSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight > upgradeHeight
	})
	s.Require().NoError(err, "state sync node failed to pass the upgrade height")

	currentChainHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get current chain height")
	status, err = stateSyncClient.Status(ctx)
	s.Require().NoError(err, "failed to get status from state sync node")
	nodeHeight := status.SyncInfo.LatestBlockHeight
	s.Require().GreaterOrEqual(nodeHeight, currentChainHeight-5, "state sync node should be within 5 blocks of the current chain tip")
	t.Logf("State sync node at height %d (current chain tip: %d)", nodeHeight, currentChainHeight)

	syncedAbciInfo, err := stateSyncClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info from state sync node")
	s.Require().Equal(targetAppVersion, syncedAbciInfo.Response.GetAppVersion(), "state sync node should have app version %d", targetAppVersion)
	t.Logf("Success: restored an app version %d snapshot and upgraded to app version %d with binary version %s",
		baseAppVersion, targetAppVersion, syncedAbciInfo.Response.GetVersion())

	// Final liveness check
	t.Log("Performing final liveness check")
	s.Require().NoError(s.CheckLiveness(ctx, chain), "liveness check failed")
	t.Log("Liveness check passed")
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
