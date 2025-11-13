package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"strconv"
	"testing"
	"time"

	addressutil "github.com/celestiaorg/tastora/framework/testutil/address"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	cometcfg "github.com/cometbft/cometbft/config"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"

	celestiadockertypes "github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
)

const (
	v2UpgradeTestHeight       = 15
	blocksBeforeUpgrade       = 10
	blocksAfterUpgrade        = 10
	blockSyncV2UpgradeTimeout = 10 * time.Minute
)

// TestBlockSyncV2Upgrade verifies that a full node can block sync from genesis
// across the v2 upgrade height. This test reproduces issue #6177 where block sync
// fails at the upgrade height with "wrong Block.Header.Version. Expected {11 1}, got {11 2}".
func (s *CelestiaTestSuite) TestBlockSyncV2Upgrade() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping block sync v2 upgrade test in short mode")
	}

	ctx := context.TODO()
	cfg := dockerchain.DefaultConfig(s.client, s.network)

	// Set genesis to start at app version 1
	cfg.Genesis = cfg.Genesis.WithAppVersion(1)

	// Build chain with validators configured with --v2-upgrade-height flag
	// The flag is set at the chain level, which applies to all validators
	// Use --timeout-commit=1s to speed up block production for faster testing
	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).
		WithAdditionalStartArgs("--force-no-bbr", "--timeout-commit=1s", "--v2-upgrade-height", strconv.FormatInt(v2UpgradeTestHeight, 10)).
		Build(ctx)
	s.Require().NoError(err, "failed to create chain")

	t.Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	// Start the chain
	err = chain.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	// Verify the chain is running
	height, err := chain.Height(ctx)
	s.Require().NoError(err, "failed to get chain height")
	s.Require().Greater(height, int64(0), "chain height is zero")

	// Get validator node to configure
	validatorNode := chain.GetNodes()[0]
	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	// Verify we're starting at app version 1
	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(uint64(1), abciInfo.Response.GetAppVersion(), "should start with app version 1")

	// Produce blocks before upgrade
	initialHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get initial height")
	s.Require().Greater(initialHeight, int64(0), "initial height is zero")

	t.Logf("Producing blocks before upgrade height. Current height: %d, Upgrade height: %d", initialHeight, v2UpgradeTestHeight)

	// Wait until we're close to the upgrade height
	if initialHeight < v2UpgradeTestHeight-blocksBeforeUpgrade {
		blocksToProduce := int(v2UpgradeTestHeight - blocksBeforeUpgrade - initialHeight)
		s.Require().NoError(wait.ForBlocks(ctx, blocksToProduce, chain), "failed to wait for blocks before upgrade")
	}

	// Verify we're still at app version 1 before upgrade
	abciInfo, err = rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(uint64(1), abciInfo.Response.GetAppVersion(), "should still be at app version 1 before upgrade")

	// Wait for upgrade to occur at v2UpgradeTestHeight
	// The upgrade happens automatically when the chain reaches the configured --v2-upgrade-height
	currentHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get current height")

	if currentHeight < v2UpgradeTestHeight {
		blocksToWait := int(v2UpgradeTestHeight - currentHeight + 2) // Add buffer
		t.Logf("Waiting for %d blocks to reach upgrade height %d", blocksToWait, v2UpgradeTestHeight)
		s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain), "failed to wait for upgrade")
	}

	// Verify upgrade completed successfully
	abciInfo, err = rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info after upgrade")
	s.Require().Equal(uint64(2), abciInfo.Response.GetAppVersion(), "should be at app version 2 after upgrade")
	t.Logf("Upgrade completed successfully at height %d", v2UpgradeTestHeight)

	// Produce additional blocks after upgrade
	t.Logf("Producing %d blocks after upgrade", blocksAfterUpgrade)
	s.Require().NoError(wait.ForBlocks(ctx, blocksAfterUpgrade, chain), "failed to wait for post-upgrade blocks")

	finalHeight, err := s.GetLatestBlockHeight(ctx, rpcClient)
	s.Require().NoError(err, "failed to get final height")
	t.Logf("Blocks 1 to %d have app version 1", v2UpgradeTestHeight-1)
	t.Logf("Blocks %d to %d have app version 2", v2UpgradeTestHeight, finalHeight)

	// Build peer list for the new node to connect to existing validators
	peerList, err := addressutil.BuildInternalPeerAddressList(ctx, chain.Nodes())
	s.Require().NoError(err, "failed to build peer address list")

	t.Logf("Latest height: %d", finalHeight)
	t.Logf("Peers: %s", peerList)

	// Add block sync node with --v2-upgrade-height flag configured
	t.Log("Adding block sync node with --v2-upgrade-height flag")
	err = chain.AddNode(ctx,
		celestiadockertypes.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			WithAdditionalStartArgs("--force-no-bbr", "--timeout-commit=1s", "--v2-upgrade-height", strconv.FormatInt(v2UpgradeTestHeight, 10)).
			WithPostInit(func(ctx context.Context, node *celestiadockertypes.ChainNode) error {
				return config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
					// disable state sync to ensure we're testing block sync
					cfg.StateSync.Enable = false
					// configure block sync
					cfg.BlockSync.Version = "v0"
					// set persistent peers to connect to existing nodes
					cfg.P2P.PersistentPeers = peerList
				})
			}).
			Build(),
	)

	s.Require().NoError(err, "failed to add block sync node")

	allNodes := chain.GetNodes()
	blockSyncNode := allNodes[len(allNodes)-1]

	s.Require().Equal(tastoratypes.NodeTypeConsensusFull, blockSyncNode.GetType(), "expected block sync node to be a full node")

	blockSyncClient, err := blockSyncNode.GetRPCClient()
	s.Require().NoError(err)

	// Wait for block sync to complete past the upgrade height
	t.Log("Waiting for block sync node to catch up past upgrade height...")
	err = s.WaitForSync(ctx, blockSyncClient, blockSyncV2UpgradeTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight >= finalHeight
	})

	s.Require().NoError(err, "failed to wait for block sync node to catch up")

	// Verify the block sync node is at the correct app version
	syncedAbciInfo, err := blockSyncClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info from block sync node")
	s.Require().Equal(uint64(2), syncedAbciInfo.Response.GetAppVersion(), "block sync node should have app version 2")
	t.Logf("Block sync node app version: %d", syncedAbciInfo.Response.GetAppVersion())

	// Verify the node can query blocks before and after upgrade height
	heightBeforeUpgrade := int64(v2UpgradeTestHeight - 1)
	blockBeforeUpgrade, err := blockSyncClient.Block(ctx, &heightBeforeUpgrade)
	s.Require().NoError(err, "failed to get block before upgrade")
	s.Require().NotNil(blockBeforeUpgrade, "block before upgrade should not be nil")
	s.Require().Equal(uint64(1), blockBeforeUpgrade.Block.Version.App, "block before upgrade should have app version 1")
	t.Logf("Successfully queried block %d (before upgrade)", heightBeforeUpgrade)

	heightAtUpgrade := int64(v2UpgradeTestHeight)
	blockAtUpgrade, err := blockSyncClient.Block(ctx, &heightAtUpgrade)
	s.Require().NoError(err, "failed to get block at upgrade height")
	s.Require().NotNil(blockAtUpgrade, "block at upgrade height should not be nil")
	s.Require().Equal(uint64(2), blockAtUpgrade.Block.Version.App, "block at upgrade should have app version 2")
	t.Logf("Successfully queried block %d (at upgrade)", heightAtUpgrade)

	heightAfterUpgrade := int64(v2UpgradeTestHeight + 1)
	blockAfterUpgrade, err := blockSyncClient.Block(ctx, &heightAfterUpgrade)
	s.Require().NoError(err, "failed to get block after upgrade")
	s.Require().NotNil(blockAfterUpgrade, "block after upgrade should not be nil")
	s.Require().Equal(uint64(2), blockAfterUpgrade.Block.Version.App, "block after upgrade should have app version 2")
	t.Logf("Successfully queried block %d (after upgrade)", heightAfterUpgrade)

	// Verify no version mismatch errors occurred by checking the node status
	status, err := blockSyncClient.Status(ctx)
	s.Require().NoError(err, "failed to get block sync node status")
	s.Require().False(status.SyncInfo.CatchingUp, "block sync node should not be catching up")
	s.Require().GreaterOrEqual(status.SyncInfo.LatestBlockHeight, finalHeight, "block sync node should be at or past final height")

	t.Logf("Block sync completed successfully. Node height: %d, Chain height: %d", status.SyncInfo.LatestBlockHeight, finalHeight)

	// Final liveness check
	t.Log("Performing final liveness check")
	s.Require().NoError(s.CheckLiveness(ctx, chain), "liveness check failed")
	t.Log("Liveness check passed")
}
