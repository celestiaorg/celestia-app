package docker_e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"

	"celestiaorg/celestia-app/test/docker-e2e/networks"
)

const (
	syncToTipTimeout = 10 * time.Minute
	mochaTrustOffset = int64(2000)
)

// TestSyncToTipMocha measures how long it takes a fresh node to sync to the
// mocha testnet tip using state sync + block sync. The KPI target is that the
// combined time stays under syncToTipTimeout (10 minutes).
func (s *CelestiaTestSuite) TestSyncToTipMocha() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()

	mochaConfig := networks.NewMochaConfig()
	mochaClient, err := networks.NewClient(mochaConfig.RPCs[0])
	s.Require().NoError(err, "failed to create mocha RPC client")

	latestHeight, err := s.GetLatestBlockHeight(ctx, mochaClient)
	s.Require().NoError(err, "failed to get latest height from mocha")

	trustHeight := latestHeight - mochaTrustOffset
	s.Require().Greater(trustHeight, int64(0), "calculated trust height %d is too low", trustHeight)

	trustBlock, err := mochaClient.Block(ctx, &trustHeight)
	s.Require().NoError(err, "failed to get block at trust height %d from mocha", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()

	t.Logf("Mocha latest height: %d", latestHeight)
	t.Logf("Using trust height: %d", trustHeight)
	t.Logf("Using trust hash: %s", trustHash)

	dockerCfg, err := networks.NewConfig(mochaConfig, s.client, s.network)
	s.Require().NoError(err, "failed to create mocha config")

	startArgs := []string{"--force-no-bbr"}
	if mochaConfig.Seeds != "" {
		startArgs = append(startArgs, fmt.Sprintf("--p2p.seeds=%s", mochaConfig.Seeds))
	}
	if mochaConfig.Peers != "" {
		startArgs = append(startArgs, fmt.Sprintf("--p2p.persistent_peers=%s", mochaConfig.Peers))
	}

	builder := networks.NewChainBuilder(s.T(), mochaConfig, dockerCfg)
	builder = builder.WithAdditionalStartArgs(startArgs...)
	mochaChain, err := builder.
		WithNodes(cosmos.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			WithPostInit(func(ctx context.Context, node *cosmos.ChainNode) error {
				return configureStateSyncClient(ctx, node, mochaConfig.RPCs, trustHeight, trustHash)
			}).
			Build(),
		).
		Build(ctx)

	s.Require().NoError(err, "failed to create chain")

	t.Log("Starting mocha sync-to-tip node")
	startTime := time.Now()

	err = mochaChain.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	t.Cleanup(func() {
		if err := mochaChain.Remove(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	allNodes := mochaChain.GetNodes()
	s.Require().Len(allNodes, 1, "expected exactly one node")
	fullNode := allNodes[0]

	syncClient, err := fullNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	t.Log("Phase 1: Waiting for state sync to reach trust height...")
	err = s.WaitForSync(ctx, syncClient, syncToTipTimeout, func(info rpctypes.SyncInfo) bool {
		return info.LatestBlockHeight >= trustHeight
	})
	s.Require().NoError(err, "state sync did not reach trust height within timeout")

	stateSyncDuration := time.Since(startTime)
	t.Logf("Phase 1 complete: state sync took %s", stateSyncDuration)

	// Verify that state sync was used.
	dockerNode := fullNode.(*cosmos.ChainNode)
	verifyStateSync(t, dockerNode)

	t.Log("Phase 2: Waiting for block sync to reach tip...")
	remainingTimeout := syncToTipTimeout - stateSyncDuration
	if remainingTimeout <= 0 {
		s.Require().Fail("no time remaining for block sync after state sync took %s", stateSyncDuration)
	}

	blockSyncStart := time.Now()
	err = s.WaitForSync(ctx, syncClient, remainingTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp
	})
	s.Require().NoError(err, "block sync did not complete within timeout")

	blockSyncDuration := time.Since(blockSyncStart)
	totalDuration := time.Since(startTime)

	t.Logf("Block sync complete: took %s", blockSyncDuration)
	t.Logf("Total sync duration: %s (state sync: %s, block sync: %s)", totalDuration, stateSyncDuration, blockSyncDuration)

	s.Require().Less(totalDuration, syncToTipTimeout,
		"total sync duration %s exceeded KPI target of %s (state sync: %s, block sync: %s)",
		totalDuration, syncToTipTimeout, stateSyncDuration, blockSyncDuration)
}
