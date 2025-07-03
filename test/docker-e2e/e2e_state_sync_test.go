package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	cometcfg "github.com/cometbft/cometbft/config"
	servercfg "github.com/cosmos/cosmos-sdk/server/config"
	"strings"
	"testing"
	"time"

	celestiadockertypes "github.com/celestiaorg/tastora/framework/docker"
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
		WithPostInit(func(ctx context.Context, node *celestiadockertypes.ChainNode) error {
			return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
				cfg.StateSync.SnapshotInterval = 5
				cfg.StateSync.SnapshotKeepRecent = 2
			})
		}).Build(ctx)
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

	initialHeight := int64(0)

	// Use a ticker to periodically check for the initial height
	heightTicker := time.NewTicker(1 * time.Second)
	defer heightTicker.Stop()

	heightTimeoutCtx, heightCancel := context.WithTimeout(ctx, 30*time.Second) // Wait up to 30 seconds for the first block
	defer heightCancel()

	// Check immediately first, then on ticker intervals
	for {
		status, err := nodeClient.Status(heightTimeoutCtx)
		if err == nil && status.SyncInfo.LatestBlockHeight > 0 {
			initialHeight = status.SyncInfo.LatestBlockHeight
			break
		}

		select {
		case <-heightTicker.C:
			// Continue the loop
		case <-heightTimeoutCtx.Done():
			t.Logf("Timed out waiting for initial height")
			break
		}
	}

	s.Require().Greater(initialHeight, int64(0), "failed to get initial height")
	targetHeight := initialHeight + blocksToProduce
	t.Logf("Successfully reached initial height %d", initialHeight)

	s.Require().NoError(wait.ForBlocks(ctx, int(targetHeight), celestia), "failed to wait for target height")

	t.Logf("Successfully reached target height %d", targetHeight)

	t.Logf("Gathering state sync parameters")
	status, err := nodeClient.Status(ctx)
	s.Require().NoError(err, "failed to get node zero client")

	latestHeight := status.SyncInfo.LatestBlockHeight
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
		celestiadockertypes.NewChainNodeConfigBuilder().
			WithNodeType(celestiadockertypes.FullNodeType).
			WithPostInit(func(ctx context.Context, node *celestiadockertypes.ChainNode) error {
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

	s.Require().Equal("fn", fullNode.GetType(), "expected state sync node to be a full node")

	stateSyncClient, err := fullNode.GetRPCClient()
	s.Require().NoError(err)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, stateSyncTimeout)
	defer cancel()

	// Check immediately first, then on ticker intervals
	for {
		status, err := stateSyncClient.Status(timeoutCtx)
		if err != nil {
			t.Logf("Failed to get status from state sync node, retrying...: %v", err)
			select {
			case <-ticker.C:
				continue
			case <-timeoutCtx.Done():
				t.Fatalf("timed out waiting for state sync node to catch up after %v", stateSyncTimeout)
			}
		}

		t.Logf("State sync node status: Height=%d, CatchingUp=%t", status.SyncInfo.LatestBlockHeight, status.SyncInfo.CatchingUp)

		if !status.SyncInfo.CatchingUp && status.SyncInfo.LatestBlockHeight >= latestHeight {
			t.Logf("State sync successful! Node caught up to height %d", status.SyncInfo.LatestBlockHeight)
			break
		}

		select {
		case <-ticker.C:
			// Continue the loop
		case <-timeoutCtx.Done():
			t.Fatalf("timed out waiting for state sync node to catch up after %v", stateSyncTimeout)
		}
	}
}
