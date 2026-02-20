package docker_e2e

import (
	"context"
	"testing"
	"time"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	"github.com/stretchr/testify/require"
)

const (
	// Test configuration
	blobSize        = 7*1024*1024 + 512*1024 // 8MB
	testDuration    = 5 * time.Minute        // 5 minutes
	submissionDelay = 1 * time.Second        // 1 tx/sec per worker = 10 tx/sec total

	// Assertions
	maxAvgLatency  = 8 * time.Second
	minSuccessRate = 0.999 // 99.9%
)

func (s *CelestiaTestSuite) TestStress8MBBlobs() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	ctx := context.Background()

	t.Logf("Starting tx submission spam test: %dMB blobs, %v duration",
		blobSize/(1024*1024), testDuration)

	// 1. Create local chain tuned for large blob stress testing:
	// - Longer TimeoutPropose: building/verifying large data squares with
	//   erasure coding is expensive on local Docker; 2s default causes
	//   consensus round escalations. 30s gives the proposer enough room.
	// - Larger mempool: at 7.5 MB/tx and 1 tx/sec we accumulate faster
	//   than the chain confirms, so we need headroom beyond 400 MiB.
	cfg := dockerchain.DefaultConfig(s.client, s.network).
		WithTag(mustGetCelestiaTag(t)).
		WithTimeoutPropose(30 * time.Second).
		WithMempoolMaxTxsBytes(10 * 1024 * 1024 * 1024). // 10 GiB
		WithAdditionalStartArgs("--delayed-precommit-timeout", "2500ms")

	celestia, err := dockerchain.NewCelestiaChainBuilder(t, cfg).Build(ctx)
	require.NoError(t, err, "failed to build chain")

	err = celestia.Start(ctx)
	require.NoError(t, err, "failed to start chain")

	t.Cleanup(func() {
		if err := celestia.Remove(ctx); err != nil {
			t.Logf("Error removing chain: %v", err)
		}
	})

	// 2. Wait for chain to be ready
	height, err := celestia.Height(ctx)
	require.NoError(t, err, "failed to get chain height")
	require.Greater(t, height, int64(0), "chain not producing blocks")
	t.Logf("Chain ready at height %d", height)

	// 3. Deploy latency-monitor container
	container, err := s.DeployLatencyMonitor(ctx, celestia, LatencyMonitorConfig{
		BlobSize:    blobSize,
		MinBlobSize: blobSize, // Fixed size
		Delay:       submissionDelay,
	})
	require.NoError(t, err, "failed to deploy latency-monitor")

	t.Logf("Running tx submission spam test for %v...", testDuration)
	time.Sleep(testDuration)

	t.Log("Collecting results (stops latency-monitor via SIGTERM)...")
	results, err := s.CollectLatencyResults(ctx, t, container.Name)
	require.NoError(t, err, "failed to collect results")

	t.Logf("Stress Test Results:")
	t.Logf("  Total Transactions: %d", results.TotalTxs)
	t.Logf("  Successful: %d (%.2f%%)", results.SuccessCount, results.SuccessRate*100)
	t.Logf("  Failed: %d", results.FailureCount)
	t.Logf("  Max Latency: %v", results.MaxLatency)
	t.Logf("  P99 Latency: %v", results.P99Latency)
	t.Logf("  Avg Latency: %v", results.AvgLatency)

	// 8. Assert expectations
	require.GreaterOrEqual(t, results.SuccessRate, minSuccessRate,
		"Success rate %.3f%% below threshold %.3f%%",
		results.SuccessRate*100, minSuccessRate*100)

	require.LessOrEqual(t, results.AvgLatency, maxAvgLatency,
		"Avg latency %v exceeds threshold %v", results.AvgLatency, maxAvgLatency)

	t.Logf("Tx submission spam test passed all invariants")
}

// mustGetCelestiaTag gets the celestia tag or fails the test
func mustGetCelestiaTag(t *testing.T) string {
	tag, err := dockerchain.GetCelestiaTagStrict()
	require.NoError(t, err, "CELESTIA_TAG must be set for this test")
	return tag
}
