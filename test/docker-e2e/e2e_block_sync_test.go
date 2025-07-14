package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"celestiaorg/celestia-app/test/docker-e2e/networks"
	"context"
	addressutil "github.com/celestiaorg/tastora/framework/testutil/address"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	cometcfg "github.com/cometbft/cometbft/config"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	"testing"
	"time"

	celestiadockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
)

const (
	blockSyncBlocksToProduce = 30
	blockSyncTimeout         = 10 * time.Minute
	// mocha block sync can take days, so we set a very long timeout
	blockSyncMochaTimeout = 7 * 24 * time.Hour // 7 days
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

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

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

	t.Logf("Latest height: %d", latestHeight)
	t.Logf("Peers: %s", peerList)

	t.Log("Adding block sync node")
	err = celestia.AddNode(ctx,
		celestiadockertypes.NewChainNodeConfigBuilder().
			WithNodeType(celestiadockertypes.FullNodeType).
			WithPostInit(func(ctx context.Context, node *celestiadockertypes.ChainNode) error {
				return config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
					// disable state sync to ensure we're testing block sync
					cfg.StateSync.Enable = false

					// configure block sync
					cfg.BlockSync.Version = "v0"

					// configure p2p for better connectivity
					cfg.P2P.AddrBookStrict = false
					cfg.P2P.AllowDuplicateIP = true
					cfg.P2P.MaxNumInboundPeers = 100
					cfg.P2P.MaxNumOutboundPeers = 100

					// set persistent peers to connect to existing nodes
					cfg.P2P.PersistentPeers = peerList
				})
			}).
			Build(),
	)

	s.Require().NoError(err, "failed to add block sync node")

	allNodes = celestia.GetNodes()
	blockSyncNode := allNodes[len(allNodes)-1]

	s.Require().Equal("fn", blockSyncNode.GetType(), "expected block sync node to be a full node")

	blockSyncClient, err := blockSyncNode.GetRPCClient()
	s.Require().NoError(err)

	err = s.WaitForSync(ctx, blockSyncClient, blockSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight >= latestHeight
	})

	s.Require().NoError(err, "failed to wait for block sync node to catch up")

}

// TestBlockSyncMocha tests block sync functionality by syncing from the mocha network.
func (s *CelestiaTestSuite) TestBlockSyncMocha() {
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

	t.Logf("Mocha latest height: %d", latestHeight)
	t.Logf("Using mocha RPC: %s", mochaConfig.RPCs[0])
	t.Logf("Using mocha seeds: %s", mochaConfig.Seeds)

	dockerCfg, err := networks.NewConfig(mochaConfig, s.client, s.network)
	s.Require().NoError(err, "failed to create mocha config")

	// create a mocha chain builder (no validators, just for block sync nodes)
	mochaChain, err := networks.NewChainBuilder(s.T(), mochaConfig, dockerCfg).
		WithNodes(celestiadockertypes.NewChainNodeConfigBuilder().
			WithNodeType(celestiadockertypes.FullNodeType).
			WithPostInit(func(ctx context.Context, node *celestiadockertypes.ChainNode) error {
				return config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
					// disable state sync to ensure we're testing block sync
					cfg.StateSync.Enable = false

					// configure block sync
					cfg.BlockSync.Version = "v0"

					// minimal p2p config like state sync test
					cfg.P2P.Seeds = mochaConfig.Seeds
				})
			}).
			Build(),
		).
		Build(ctx)

	s.Require().NoError(err, "failed to create chain")

	t.Log("Starting mocha block sync node")

	// use a timeout context for starting the chain
	startCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	t.Cleanup(func() {
		if err := mochaChain.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	err = mochaChain.Start(startCtx)
	s.Require().NoError(err, "failed to start chain")

	allNodes := mochaChain.GetNodes()
	s.Require().Len(allNodes, 1, "expected exactly one node")
	blockSyncNode := allNodes[0]

	s.Require().Equal("fn", blockSyncNode.GetType(), "expected block sync node to be a full node")

	blockSyncClient, err := blockSyncNode.GetRPCClient()
	s.Require().NoError(err, "failed to get block sync client")

	// verify the node is responsive before starting sync wait
	_, err = blockSyncClient.Status(ctx)
	s.Require().NoError(err, "block sync node is not responsive")

	t.Log("Waiting for block sync to complete (this may take several days)")
	err = s.WaitForSync(ctx, blockSyncClient, blockSyncMochaTimeout, func(info rpctypes.SyncInfo) bool {
		// log progress periodically
		if info.LatestBlockHeight%1000 == 0 {
			t.Logf("Block sync progress: height %d, catching up: %v", info.LatestBlockHeight, info.CatchingUp)
		}
		// we consider sync complete when we're within 100 blocks of the latest height
		// and not catching up anymore
		return !info.CatchingUp && (latestHeight-info.LatestBlockHeight) < 100
	})

	s.Require().NoError(err, "failed to wait for block sync to complete")
}
