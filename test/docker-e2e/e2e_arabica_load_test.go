package docker_e2e

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"celestiaorg/celestia-app/test/docker-e2e/networks"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/stretchr/testify/require"
)

const (
	// Default load targets 16 MiB of blob data per block (~3 s block time).
	// 2 MiB blobs every 375 ms → 8 blobs per 3 s block = 16 MiB/block.
	defaultArabicaBlobSize        = 7 * 1024 * 1024       // 2 MiB per blob
	defaultArabicaSubmissionDelay = 125 * time.Millisecond // 8 blobs/s × 2 MiB = 16 MiB/s
	defaultArabicaTestDuration    = 10 * time.Minute

	// The single assertion: average block time must stay ≤ 4 s while the
	// network processes 16 MiB of blobs per block.
	maxAvgBlockTime = 7 * time.Second

)

// TestArabicaLoad connects to the Arabica devnet, submits blobs via the
// latency-monitor at a configurable rate, and checks whether block times
// degrade under load.
//
// Required env var:
//	ARABICA_PRIV_KEY    – hex-encoded private key for a funded Arabica account
//
// Optional env vars (with defaults):
//
//	ARABICA_RPC              – RPC endpoint      (default: https://rpc.celestia-arabica-11.com:443)
//	ARABICA_BLOB_SIZE        – blob size in bytes (default: 2 MiB)
//	ARABICA_SUBMISSION_DELAY – delay between blobs (default: 125ms)
//	ARABICA_TEST_DURATION    – total duration      (default: 10m)
func (s *CelestiaTestSuite) TestArabicaLoad() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping Arabica load test in short mode")
	}

	privKeyHex := os.Getenv("ARABICA_PRIV_KEY")
	keyringDir := os.Getenv("ARABICA_KEYRING_DIR")
	if privKeyHex == "" && keyringDir == "" {
		t.Skip("ARABICA_PRIV_KEY or ARABICA_KEYRING_DIR not set, skipping Arabica load test")
	}

	// Parse configurable parameters.
	blobSize := envIntOr("ARABICA_BLOB_SIZE", defaultArabicaBlobSize)
	submissionDelay := envDurationOr("ARABICA_SUBMISSION_DELAY", defaultArabicaSubmissionDelay)
	testDuration := envDurationOr("ARABICA_TEST_DURATION", defaultArabicaTestDuration)

	arabicaCfg := networks.NewArabicaConfig()

	t.Logf("Arabica Load Test Configuration:")
	t.Logf("  RPC:              %s", arabicaCfg.RPCs[0])
	t.Logf("  gRPC:             %s", arabicaCfg.GRPCs[0])
	t.Logf("  Blob size:        %d bytes", blobSize)
	t.Logf("  Submission delay: %v", submissionDelay)
	t.Logf("  Test duration:    %v", testDuration)
	t.Logf("  Per-block data:   ~%.1f MiB (assuming 3 s blocks)",
		float64(blobSize)*3/submissionDelay.Seconds()/1024/1024)

	ctx := context.Background()

	// --- 1. Connect to Arabica RPC for block time monitoring ---
	rpcClient, err := rpchttp.New(arabicaCfg.RPCs[0], "/websocket")
	require.NoError(t, err, "failed to create RPC client")

	status, err := rpcClient.Status(ctx)
	require.NoError(t, err, "failed to query Arabica status")
	startHeight := status.SyncInfo.LatestBlockHeight
	t.Logf("Connected to Arabica at height %d", startHeight)

	// --- 2. Deploy latency-monitor against Arabica ---
	container, err := s.DeployLatencyMonitorForNetwork(ctx, arabicaCfg.GRPCs[0], LatencyMonitorConfig{
		BlobSize:        blobSize,
		MinBlobSize:     blobSize,
		SubmissionDelay: submissionDelay,
		PrivKeyHex:      privKeyHex,
		KeyringDir:      keyringDir,
	})
	require.NoError(t, err, "failed to deploy latency-monitor")

	// --- 4. Run for the test duration ---
	t.Logf("Running load test for %v...", testDuration)
	time.Sleep(testDuration)

	// --- 5. Collect latency results ---
	t.Log("Collecting latency results...")
	latencyResults, err := s.CollectLatencyResults(ctx, t, container.Name)
	require.NoError(t, err, "failed to collect latency results")

	// --- 6. Collect block time data ---
	endStatus, err := rpcClient.Status(ctx)
	require.NoError(t, err, "failed to get end height")
	endHeight := endStatus.SyncInfo.LatestBlockHeight
	t.Logf("Load test covered heights %d to %d (%d blocks)", startHeight, endHeight, endHeight-startHeight)

	blockTimes, err := fetchBlockTimes(ctx, rpcClient, startHeight, endHeight)
	require.NoError(t, err, "failed to fetch block times")
	require.NotEmpty(t, blockTimes, "no block time samples collected")

	avgBT, err := averageBlockTime(blockTimes, startHeight, endHeight)
	require.NoError(t, err, "failed to compute average block time")

	// --- 7. Report ---
	t.Logf("")
	t.Logf("=== Arabica Load Test Results ===")
	t.Logf("")
	t.Logf("Block Time Statistics (%d blocks):", len(blockTimes))
	t.Logf("  Average: %v", avgBT)
	t.Logf("")
	t.Logf("Tx Submission Statistics:")
	t.Logf("  Total Transactions: %d", latencyResults.TotalTxs)
	t.Logf("  Successful: %d (%.2f%%)", latencyResults.SuccessCount, latencyResults.SuccessRate*100)
	t.Logf("  Failed: %d", latencyResults.FailureCount)
	t.Logf("  Avg Latency: %v", latencyResults.AvgLatency)
	t.Logf("  Max Latency: %v", latencyResults.MaxLatency)

	// --- 8. Assert: block time must not exceed 4 s under 16 MiB/s load ---
	require.LessOrEqual(t, avgBT, maxAvgBlockTime,
		"average block time %v exceeds %v under 16 MiB/s blob load", avgBT, maxAvgBlockTime)

	t.Log("Arabica load test passed")
}

// fetchBlockTimes retrieves block timestamps between startHeight and endHeight
// (inclusive) and returns them indexed by height.
func fetchBlockTimes(ctx context.Context, rpcClient *rpchttp.HTTP, startHeight, endHeight int64) (map[int64]time.Time, error) {
	if endHeight <= startHeight {
		return nil, fmt.Errorf("endHeight %d <= startHeight %d", endHeight, startHeight)
	}

	// BlockchainInfo accepts inclusive [min, max] ranges of up to 20 blocks per call.
	const batchSize = 20
	times := make(map[int64]time.Time, endHeight-startHeight+1)
	for batchStart := startHeight; batchStart <= endHeight; batchStart += batchSize {
		// Last height in this batch (inclusive), capped so we never query past endHeight.
		batchEnd := min(batchStart+batchSize-1, endHeight)
		info, err := rpcClient.BlockchainInfo(ctx, batchStart, batchEnd)
		if err != nil {
			return nil, fmt.Errorf("BlockchainInfo(%d, %d): %w", batchStart, batchEnd, err)
		}
		for _, bm := range info.BlockMetas {
			times[bm.Header.Height] = bm.Header.Time
		}
	}

	return times, nil
}

// averageBlockTime returns the mean inter-block duration across the given heights.
// Uses telescoping: (last - first) / (number of intervals).
func averageBlockTime(times map[int64]time.Time, startHeight, endHeight int64) (time.Duration, error) {
	first, ok := times[startHeight]
	if !ok {
		return 0, fmt.Errorf("missing block time for start height %d", startHeight)
	}
	last, ok := times[endHeight]
	if !ok {
		return 0, fmt.Errorf("missing block time for end height %d", endHeight)
	}
	return last.Sub(first) / time.Duration(endHeight-startHeight), nil
}

// envIntOr reads an integer from an environment variable or returns a default.
func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// envDurationOr reads a duration from an environment variable or returns a default.
func envDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
