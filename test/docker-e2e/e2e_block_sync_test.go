package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"testing"
	"time"

	addressutil "github.com/celestiaorg/tastora/framework/testutil/address"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	cometcfg "github.com/cometbft/cometbft/config"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"

	celestiadockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
)

const (
	blockSyncBlocksToProduce = 30
	blockSyncTimeout         = 10 * time.Minute
)

func (s *CelestiaTestSuite) TestBlockSync() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()
	cfg := dockerchain.DefaultConfig(s.client, s.network)
	celestia, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	s.Require().NoError(err, "failed to create chain")

	t.Cleanup(func() {
		if err := celestia.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

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

	targetHeight := initialHeight + blockSyncBlocksToProduce
	t.Logf("Successfully reached initial height %d", initialHeight)

	s.Require().NoError(wait.ForBlocks(ctx, int(targetHeight), celestia), "failed to wait for target height")

	t.Logf("Successfully reached target height %d", targetHeight)

	// get the latest height and block info for the block sync node
	latestHeight, err := s.GetLatestBlockHeight(ctx, nodeClient)
	s.Require().NoError(err, "failed to get latest height")
	s.Require().Greater(latestHeight, int64(0), "latest height is zero")

	// build peer list for the new node to connect to existing validators
	peerList, err := addressutil.BuildInternalPeerAddressList(ctx, celestia.GetNodes())
	s.Require().NoError(err, "failed to build peer address list")

	t.Logf("Latest height: %d", latestHeight)
	t.Logf("Peers: %s", peerList)

	t.Log("Adding block sync node")
	err = celestia.AddNode(ctx,
		celestiadockertypes.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
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

	allNodes = celestia.GetNodes()
	blockSyncNode := allNodes[len(allNodes)-1]

	s.Require().Equal(tastoratypes.NodeTypeConsensusFull, blockSyncNode.GetType(), "expected block sync node to be a full node")

	blockSyncClient, err := blockSyncNode.GetRPCClient()
	s.Require().NoError(err)

	err = s.WaitForSync(ctx, blockSyncClient, blockSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight >= latestHeight
	})

	s.Require().NoError(err, "failed to wait for block sync node to catch up")

	s.T().Logf("Checking validator liveness from height %d", initialHeight)
	s.Require().NoError(
		s.CheckLiveness(ctx, celestia),
		"validator liveness check failed",
	)
}
