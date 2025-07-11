package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
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

	initialHeight := int64(0)

	// use a ticker to periodically check for the initial height
	heightTicker := time.NewTicker(1 * time.Second)
	defer heightTicker.Stop()

	heightTimeoutCtx, heightCancel := context.WithTimeout(ctx, 30*time.Second)
	defer heightCancel()

	// check immediately first, then on ticker intervals
	for {
		status, err := nodeClient.Status(heightTimeoutCtx)
		if err == nil && status.SyncInfo.LatestBlockHeight > 0 {
			initialHeight = status.SyncInfo.LatestBlockHeight
			break
		}

		select {
		case <-heightTicker.C:
			// continue the loop
		case <-heightTimeoutCtx.Done():
			t.Logf("Timed out waiting for initial height")
			break
		}
	}

	s.Require().Greater(initialHeight, int64(0), "failed to get initial height")
	targetHeight := initialHeight + blockSyncBlocksToProduce
	t.Logf("Successfully reached initial height %d", initialHeight)

	s.Require().NoError(wait.ForBlocks(ctx, int(targetHeight), celestia), "failed to wait for target height")

	t.Logf("Successfully reached target height %d", targetHeight)

	// get the latest height and block info for the block sync node
	status, err := nodeClient.Status(ctx)
	s.Require().NoError(err, "failed to get node status")

	latestHeight := status.SyncInfo.LatestBlockHeight

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
