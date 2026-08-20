package docker_e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"celestiaorg/celestia-app/test/docker-e2e/networks"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	jsonrpcclient "github.com/cometbft/cometbft/rpc/jsonrpc/client"
	"github.com/stretchr/testify/require"
)

const (
	// Default load: 5 MiB blobs every 250 ms (sequential, no workers).
	defaultCortoBlobSize        = 5 * 1024 * 1024        // 5 MiB per blob
	defaultCortoSubmissionDelay = 250 * time.Millisecond // 4 blobs/s × 5 MiB
	defaultCortoTestDuration    = 10 * time.Minute

	// Parallel worker accounts. 1 = sequential submission (broadcast only, no
	// per-tx confirmation wait on the critical path). Values >1 enable parallel
	// workers, but each worker then blocks until its tx is committed, so the
	// submission ceiling becomes workers / confirmation_time. Worker accounts
	// are auto-created, funded, and fee-granted from the master account.
	defaultCortoWorkers = 1

	// Assertions: both the average and the worst single inter-block interval
	// must stay ≤ 4 s while the network processes 20 MiB of blobs per second.
	maxAvgBlockTime    = 4 * time.Second
	maxSingleBlockTime = 4 * time.Second
)

// TestCortoLoad connects to the Corto internal testnet, submits blobs via the
// latency-monitor at a configurable rate, and checks whether block times
// degrade under load.
//
// Required env vars:
//
//	CORTO_PRIV_KEY – hex-encoded private key for a funded Corto account
//	CORTO_RPC      – RPC endpoint
//	CORTO_GRPC     – gRPC endpoint
//
// Optional env vars (with defaults):
//
//	CORTO_AUTH_TOKEN       – auth token sent as an x-token header on gRPC calls
//	                         and as an Authorization Bearer header on RPC requests
//	CORTO_KEYRING_DIR      – keyring directory (alternative to CORTO_PRIV_KEY)
//	CORTO_BLOB_SIZE        – blob size in bytes (default: 5 MiB)
//	CORTO_SUBMISSION_DELAY – delay between blobs (default: 250ms)
//	CORTO_TEST_DURATION    – total duration      (default: 10m)
//	CORTO_WORKERS          – parallel worker accounts (default: 1 = sequential)
func (s *CelestiaTestSuite) TestCortoLoad() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping Corto load test in short mode")
	}

	privKeyHex := os.Getenv("CORTO_PRIV_KEY")
	keyringDir := os.Getenv("CORTO_KEYRING_DIR")
	if privKeyHex == "" && keyringDir == "" {
		t.Skip("CORTO_PRIV_KEY or CORTO_KEYRING_DIR not set, skipping Corto load test")
	}
	if os.Getenv("CORTO_RPC") == "" || os.Getenv("CORTO_GRPC") == "" {
		t.Skip("CORTO_RPC or CORTO_GRPC not set, skipping Corto load test")
	}

	// Parse configurable parameters.
	blobSize := envIntOr("CORTO_BLOB_SIZE", defaultCortoBlobSize)
	submissionDelay := envDurationOr("CORTO_SUBMISSION_DELAY", defaultCortoSubmissionDelay)
	testDuration := envDurationOr("CORTO_TEST_DURATION", defaultCortoTestDuration)
	workers := envIntOr("CORTO_WORKERS", defaultCortoWorkers)

	cortoCfg, err := networks.NewCortoConfig()
	require.NoError(t, err, "failed to build Corto config")

	// A configured token implies a TLS-terminating auth proxy, whatever the
	// port. Unauthenticated endpoints on port 443 (e.g.
	// grpc.celestia-corto.com:443) are also dialed with TLS.
	useTLS := cortoCfg.AuthToken != "" || strings.HasSuffix(cortoCfg.GRPCs[0], ":443")

	t.Logf("Corto Load Test Configuration:")
	t.Logf("  RPC:              %s (auth=%v)", cortoCfg.RPCs[0], cortoCfg.AuthToken != "")
	t.Logf("  gRPC:             %s (tls=%v, auth=%v)", cortoCfg.GRPCs[0], useTLS, cortoCfg.AuthToken != "")
	t.Logf("  Blob size:        %d bytes", blobSize)
	t.Logf("  Submission delay: %v", submissionDelay)
	t.Logf("  Workers:          %d", workers)
	t.Logf("  Test duration:    %v", testDuration)
	t.Logf("  Per-block data:   ~%.1f MiB (assuming 3 s blocks)",
		float64(blobSize)*3/submissionDelay.Seconds()/1024/1024)

	ctx := context.Background()

	// --- 1. Connect to Corto RPC for block time monitoring ---
	rpcClient, err := newAuthedRPCClient(cortoCfg.RPCs[0], cortoCfg.AuthToken)
	require.NoError(t, err, "failed to create RPC client")

	// Corto is an internal testnet: when the test is configured to run against
	// it, an unreachable endpoint is a real failure, not a reason to skip.
	status, err := rpcClient.Status(ctx)
	require.NoError(t, err, "Corto endpoint %s unavailable", cortoCfg.RPCs[0])
	startHeight := status.SyncInfo.LatestBlockHeight
	t.Logf("Connected to Corto at height %d", startHeight)

	// --- 2. Deploy latency-monitor against Corto ---
	container, err := s.DeployLatencyMonitorForNetwork(ctx, cortoCfg.GRPCs[0], LatencyMonitorConfig{
		BlobSize:        blobSize,
		MinBlobSize:     blobSize,
		SubmissionDelay: submissionDelay,
		Workers:         workers,
		PrivKeyHex:      privKeyHex,
		KeyringDir:      keyringDir,
		TLS:             useTLS,
		AuthToken:       cortoCfg.AuthToken,
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

	maxBT := maxBlockTime(blockTimes, startHeight, endHeight)

	// --- 7. Report ---
	t.Logf("")
	t.Logf("=== Corto Load Test Results ===")
	t.Logf("")
	t.Logf("Block Time Statistics (%d blocks):", len(blockTimes))
	t.Logf("  Average: %v", avgBT)
	t.Logf("  Max:     %v", maxBT)
	t.Logf("")
	t.Logf("Tx Submission Statistics:")
	t.Logf("  Total Transactions: %d", latencyResults.TotalTxs)
	t.Logf("  Successful: %d (%.2f%%)", latencyResults.SuccessCount, latencyResults.SuccessRate*100)
	t.Logf("  Failed: %d", latencyResults.FailureCount)
	t.Logf("  Avg Latency: %v", latencyResults.AvgLatency)
	t.Logf("  Max Latency: %v", latencyResults.MaxLatency)

	// --- 8. Assert: block time must not exceed 4 s under 20 MiB/s load ---
	require.LessOrEqual(t, avgBT, maxAvgBlockTime,
		"average block time %v exceeds %v under 20 MiB/s blob load", avgBT, maxAvgBlockTime)
	require.LessOrEqual(t, maxBT, maxSingleBlockTime,
		"max block time %v exceeds %v under 20 MiB/s blob load", maxBT, maxSingleBlockTime)

	t.Log("Corto load test passed")
}

// newAuthedRPCClient returns an RPC client for the given endpoint that
// attaches the token as an Authorization Bearer header when set.
func newAuthedRPCClient(remote, token string) (*rpchttp.HTTP, error) {
	if token != "" && !strings.HasPrefix(remote, "https://") {
		return nil, fmt.Errorf("an auth token is set but RPC endpoint %s is not https: refusing to send the token over plaintext", remote)
	}
	httpClient, err := jsonrpcclient.DefaultHTTPClient(remote)
	if err != nil {
		return nil, err
	}
	if token != "" {
		httpClient.Transport = &bearerRoundTripper{token: token, base: httpClient.Transport}
	}
	return rpchttp.NewWithClient(remote, "/websocket", httpClient)
}

// bearerRoundTripper attaches an Authorization Bearer header to every request.
type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (b *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(clone)
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
	for batchStart := startHeight; batchStart <= endHeight; {
		// Last height in this batch (inclusive), capped so we never query past endHeight.
		batchEnd := min(batchStart+batchSize-1, endHeight)
		info, err := rpcClient.BlockchainInfo(ctx, batchStart, batchEnd)
		if err != nil {
			return nil, fmt.Errorf("BlockchainInfo(%d, %d): %w", batchStart, batchEnd, err)
		}
		for _, bm := range info.BlockMetas {
			times[bm.Header.Height] = bm.Header.Time
		}
		batchStart = batchEnd + 1 // next window starts right after this one
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

// maxBlockTime returns the largest interval between two consecutive blocks in
// [startHeight, endHeight].
func maxBlockTime(times map[int64]time.Time, startHeight, endHeight int64) time.Duration {
	var maxBT time.Duration
	for h := startHeight + 1; h <= endHeight; h++ {
		cur, curOK := times[h]
		prev, prevOK := times[h-1]
		if !curOK || !prevOK {
			continue
		}
		if bt := cur.Sub(prev); bt > maxBT {
			maxBT = bt
		}
	}
	return maxBT
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
