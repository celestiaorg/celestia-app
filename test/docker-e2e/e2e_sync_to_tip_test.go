package docker_e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"

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

	metrics := newSyncMetrics("mocha")
	defer metrics.push(t)

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

	// fetch live peers from RPC /net_info endpoints so we don't rely on a
	// hardcoded list that goes stale over time.
	peers := networks.FetchPeers(t, mochaConfig.RPCs, 10)

	startArgs := []string{"--force-no-bbr"}
	if mochaConfig.Seeds != "" {
		startArgs = append(startArgs, fmt.Sprintf("--p2p.seeds=%s", mochaConfig.Seeds))
	}
	if peers != "" {
		startArgs = append(startArgs, fmt.Sprintf("--p2p.persistent_peers=%s", peers))
	}

	builder := networks.NewChainBuilder(s.T(), mochaConfig, dockerCfg)
	builder = builder.WithAdditionalStartArgs(startArgs...).
		WithBlockWaitTimeout(syncToTipTimeout)
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
	metrics.recordPhase("state_sync", stateSyncDuration)
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
	metrics.recordPhase("block_sync", blockSyncDuration)
	metrics.recordPhase("total", totalDuration)

	t.Logf("Block sync complete: took %s", blockSyncDuration)
	t.Logf("Total sync duration: %s (state sync: %s, block sync: %s)", totalDuration, stateSyncDuration, blockSyncDuration)

	s.Require().Less(totalDuration, syncToTipTimeout,
		"total sync duration %s exceeded KPI target of %s (state sync: %s, block sync: %s)",
		totalDuration, syncToTipTimeout, stateSyncDuration, blockSyncDuration)

	metrics.markSuccess()
}

const pushJobName = "celestia_e2e_sync_to_tip"

type syncMetrics struct {
	network string
	phases  map[string]time.Duration
	success bool
}

func newSyncMetrics(network string) *syncMetrics {
	return &syncMetrics{network: network, phases: make(map[string]time.Duration)}
}

func (m *syncMetrics) recordPhase(phase string, d time.Duration) {
	m.phases[phase] = d
}

func (m *syncMetrics) markSuccess() {
	m.success = true
}

// push sends the recorded sync durations and success flag to the Prometheus
// Pushgateway specified by PUSHGATEWAY_URL. If the env var is unset the push is
// skipped so local runs and PR CI don't try to reach an external service. Push
// failures are logged but do not fail the test — the test result reflects the
// sync KPI, not observability health.
func (m *syncMetrics) push(t *testing.T) {
	t.Helper()
	url := os.Getenv("PUSHGATEWAY_URL")
	if url == "" {
		t.Log("PUSHGATEWAY_URL not set; skipping metrics push")
		return
	}

	labels := []string{"commit_sha", "github_run_id", "celestia_app_version"}
	durationGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "celestia_e2e_sync_duration_seconds",
		Help: "Time taken for each phase of the sync-to-tip e2e test.",
	}, append([]string{"phase"}, labels...))
	successGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "celestia_e2e_sync_success",
		Help: "Whether the most recent sync-to-tip e2e run met its KPI (1 = success, 0 = failure).",
	}, labels)

	commitSHA := os.Getenv("GITHUB_SHA")
	if len(commitSHA) > 8 {
		commitSHA = commitSHA[:8]
	}
	labelVals := []string{commitSHA, os.Getenv("GITHUB_RUN_ID"), os.Getenv("CELESTIA_APP_VERSION")}

	for phase, d := range m.phases {
		durationGauge.WithLabelValues(append([]string{phase}, labelVals...)...).Set(d.Seconds())
	}
	var successVal float64
	if m.success {
		successVal = 1
	}
	successGauge.WithLabelValues(labelVals...).Set(successVal)

	pusher := push.New(url, pushJobName).
		Grouping("network", m.network).
		Collector(durationGauge).
		Collector(successGauge)
	if user := os.Getenv("PUSHGATEWAY_USERNAME"); user != "" {
		pusher = pusher.BasicAuth(user, os.Getenv("PUSHGATEWAY_PASSWORD"))
	}

	if err := pusher.Push(); err != nil {
		t.Logf("failed to push metrics to Pushgateway: %v", err)
		return
	}
	t.Logf("pushed sync metrics to Pushgateway at %s (success=%v)", url, m.success)
}
