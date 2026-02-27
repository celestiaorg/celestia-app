package docker_e2e

import (
	"context"
	"time"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	"github.com/stretchr/testify/require"
)

const (
	blobSize        = 7*1024*1024 + 512*1024 // 7.5 MiB
	testDuration    = 15 * time.Minute       // 15 minutes
	submissionDelay = 1 * time.Second

	maxAvgLatency  = 8 * time.Second
	minSuccessRate = 0.999 // 99.9%
)

func (s *CelestiaTestSuite) TestTxSubmission() {
	t := s.T()

	ctx := context.Background()

	t.Logf("Starting tx submission test: %d MiB blob size, %v duration",
		blobSize/(1024*1024), testDuration)

	cfg := dockerchain.DefaultConfig(s.client, s.network).
		WithTag(dockerchain.GetCelestiaTag())

	celestia, err := dockerchain.NewCelestiaChainBuilder(t, cfg).Build(ctx)
	require.NoError(t, err, "failed to build chain")

	err = celestia.Start(ctx)
	require.NoError(t, err, "failed to start chain")

	t.Cleanup(func() {
		if err := celestia.Remove(ctx); err != nil {
			t.Logf("Error removing chain: %v", err)
		}
	})

	// Wait for chain to be ready
	height, err := celestia.Height(ctx)
	require.NoError(t, err, "failed to get chain height")
	require.Greater(t, height, int64(0), "chain not producing blocks")
	t.Logf("Chain ready at height %d", height)

	// Deploy latency-monitor container
	container, err := s.DeployLatencyMonitor(ctx, celestia, LatencyMonitorConfig{
		BlobSize:        blobSize,
		MinBlobSize:     blobSize,
		SubmissionDelay: submissionDelay,
	})
	require.NoError(t, err, "failed to deploy latency-monitor")

	t.Logf("Running tx submission test for %v...", testDuration)
	time.Sleep(testDuration)

	t.Log("Collecting results...")
	results, err := s.CollectLatencyResults(ctx, t, container.Name)
	require.NoError(t, err, "failed to collect results")

	t.Logf("Tx Submission Test Results:")
	t.Logf("  Total Transactions: %d", results.TotalTxs)
	t.Logf("  Successful: %d (%.2f%%)", results.SuccessCount, results.SuccessRate*100)
	t.Logf("  Failed: %d", results.FailureCount)
	t.Logf("  Max Latency: %v", results.MaxLatency)
	t.Logf("  Avg Latency: %v", results.AvgLatency)

	require.GreaterOrEqual(t, results.SuccessRate, minSuccessRate,
		"Success rate %.3f%% below threshold %.3f%%",
		results.SuccessRate*100, minSuccessRate*100)

	require.LessOrEqual(t, results.AvgLatency, maxAvgLatency,
		"Avg latency %v exceeds threshold %v", results.AvgLatency, maxAvgLatency)

	t.Logf("Tx submission test passed all invariants")
}
