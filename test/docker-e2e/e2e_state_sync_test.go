package docker_e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	addressutil "github.com/celestiaorg/tastora/framework/testutil/address"
	"github.com/celestiaorg/tastora/framework/testutil/config"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	cometcfg "github.com/cometbft/cometbft/config"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	servercfg "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/stretchr/testify/require"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"celestiaorg/celestia-app/test/docker-e2e/networks"
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
		WithPostInit(validatorStateSyncProducerOverrides).
		Build(ctx)

	s.Require().NoError(err, "failed to get chain")

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	// Cleanup resources when the test is done
	t.Cleanup(func() {
		if err := celestia.Remove(ctx); err != nil {
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
		cosmos.NewChainNodeConfigBuilder().
			WithNodeType(tastoratypes.NodeTypeConsensusFull).
			WithPostInit(func(ctx context.Context, node *cosmos.ChainNode) error {
				return configureStateSyncClient(ctx, node, strings.Split(rpcServers, ","), trustHeight, trustHash)
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

	// Verify that state sync was used (not block sync) via metrics
	dockerNode := fullNode.(*cosmos.ChainNode)
	verifyStateSync(t, dockerNode)

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

	latestHeight, err := s.GetLatestBlockHeight(ctx, mochaClient)
	s.Require().NoError(err, "failed to get latest height from mocha")

	trustHeight := latestHeight - 2000
	s.Require().Greater(trustHeight, int64(0), "calculated trust height %d is too low", trustHeight)

	trustBlock, err := mochaClient.Block(ctx, &trustHeight)
	s.Require().NoError(err, "failed to get block at trust height %d from mocha", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()

	t.Logf("Mocha latest height: %d", latestHeight)
	t.Logf("Using trust height: %d", trustHeight)
	t.Logf("Using trust hash: %s", trustHash)
	t.Logf("Using mocha RPC: %s", mochaConfig.RPCs[0])

	dockerCfg, err := networks.NewConfig(mochaConfig, s.client, s.network)
	s.Require().NoError(err, "failed to create mocha config")

	// Pass seeds and peers via CLI flags (CometBFT reads CLI args correctly, config file had issues)
	startArgs := []string{"--force-no-bbr"}
	if mochaConfig.Seeds != "" {
		startArgs = append(startArgs, fmt.Sprintf("--p2p.seeds=%s", mochaConfig.Seeds))
		t.Logf("Adding seeds via CLI: %s", mochaConfig.Seeds)
	}
	if mochaConfig.Peers != "" {
		startArgs = append(startArgs, fmt.Sprintf("--p2p.persistent_peers=%s", mochaConfig.Peers))
		t.Logf("Adding persistent peers via CLI: %d peers", len(strings.Split(mochaConfig.Peers, ",")))
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

	t.Log("Starting mocha state sync node")
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

	s.Require().Equal(tastoratypes.NodeTypeConsensusFull, fullNode.GetType(), "expected state sync node to be a full node")

	stateSyncClient, err := fullNode.GetRPCClient()
	s.Require().NoError(err, "failed to get state sync client")

	err = s.WaitForSync(ctx, stateSyncClient, stateSyncTimeout, func(info rpctypes.SyncInfo) bool {
		return !info.CatchingUp && info.LatestBlockHeight >= trustHeight
	})

	s.Require().NoError(err, "failed to wait for state sync to complete")

	// Verify that state sync was used (not block sync) via metrics
	dockerNode := fullNode.(*cosmos.ChainNode)
	verifyStateSync(t, dockerNode)
}

// validatorStateSyncProducerOverrides configures validators to produce state sync snapshots.
func validatorStateSyncProducerOverrides(ctx context.Context, node *cosmos.ChainNode) error {
	return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
		cfg.StateSync.SnapshotInterval = 5
		cfg.StateSync.SnapshotKeepRecent = 3
	})
}

// configureStateSyncClient configures a node to use state sync.
func configureStateSyncClient(ctx context.Context, node *cosmos.ChainNode, rpcEndpoints []string, trustHeight int64, trustHash string) error {
	err := config.Modify(ctx, node, "config/config.toml", func(cfg *cometcfg.Config) {
		cfg.StateSync.Enable = true

		if len(rpcEndpoints) > 0 {
			cfg.StateSync.RPCServers = rpcEndpoints
		}

		cfg.StateSync.TrustHeight = trustHeight
		cfg.StateSync.TrustHash = trustHash

		cfg.StateSync.TrustPeriod = 168 * time.Hour // 1 week
		cfg.StateSync.DiscoveryTime = 5 * time.Second

		cfg.Instrumentation.Prometheus = true
		cfg.Instrumentation.PrometheusListenAddr = "0.0.0.0:26660"
	})
	if err != nil {
		return err
	}

	return config.Modify(ctx, node, "config/app.toml", func(cfg *servercfg.Config) {
		cfg.Telemetry.Enabled = true
	})
}

// detectStateSyncFromMetrics queries Prometheus metrics to determine if state sync was used
func detectStateSyncFromMetrics(t *testing.T, node *cosmos.ChainNode) (usedStateSync bool, err error) {
	ctx := context.Background()

	networkInfo, err := node.GetNetworkInfo(ctx)
	require.NoError(t, err, "failed to get network info from chain node")
	hostname := networkInfo.Internal.Hostname

	// NOTE: Due to Tastora's limitation, we must use curl to fetch metrics from the node.
	// Once the port issue is resolved, we can fetch metrics directly from the node without curl.
	endpoint := fmt.Sprintf("http://%s:26660/metrics", hostname)
	cmd := []string{"curl", "--silent", "--connect-timeout", "10", "--max-time", "30", endpoint}
	stdout, stderr, execErr := node.Exec(ctx, cmd, nil)

	if execErr != nil {
		return false, fmt.Errorf("failed to fetch metrics from %s: %v, stderr: %s", endpoint, execErr, string(stderr))
	}

	metrics := string(stdout)
	if len(metrics) == 0 {
		return false, fmt.Errorf("received empty metrics response from %s", endpoint)
	}

	return findStateSyncMetrics(t, metrics)
}

func findStateSyncMetrics(t *testing.T, metrics string) (usedStateSync bool, err error) {
	// Check for state sync evidence
	// The presence of apply_snapshot_chunk metrics with non-zero count proves state sync was used
	lines := strings.Split(metrics, "\n")
	for _, line := range lines {
		if strings.Contains(line, "apply_snapshot_chunk") && strings.Contains(line, "_count{") {
			// Look for non-zero count indicating snapshot chunks were applied
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				countStr := parts[len(parts)-1]
				if countStr != "0" && countStr != "0.0" {
					t.Logf("State sync confirmed: applied %s snapshot chunks", countStr)
					return true, nil
				}
			}
		}
	}

	// No evidence of state sync found
	return false, nil
}

// verifyStateSync validates that state sync was used and not block sync
func verifyStateSync(t *testing.T, stateSyncNode *cosmos.ChainNode) {
	t.Log("Verifying sync method via Prometheus metrics...")

	usedStateSync, metricsErr := detectStateSyncFromMetrics(t, stateSyncNode)
	if metricsErr != nil {
		t.Fatalf("Failed to verify sync method via metrics: %v", metricsErr)
	}

	if !usedStateSync {
		t.Fatal("Failed to confirm state sync was used via Prometheus metrics")
	}

	t.Log("Success: Prometheus metrics confirm state sync was used")
}
