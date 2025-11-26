package docker_e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/latency"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker/cosmos"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
)

const (
	// Nightly test configuration constants
	nightlyTestDuration = 15 * time.Minute    // 15 minutes as specified in requirements
	submissionDelay     = 1 * time.Second     // 4 second delay between submissions
	latencyThreshold    = 30 * time.Second    // 30 second latency threshold for regression detection
	blobSize            = appconsts.MaxTxSize // 8MB blobs
	minBlobSize         = 1000                // 1KB
)

// TestE2ENightlyLatency runs the nightly transaction latency test
// It should test:
// - Sequential transaction submission from a single account (8Mib blobs every second)
// - 15-minute continuous test duration
// - Latency measurement and statistics
// - 100% success rate requirement
// - Decects confirmation latency regressions 
func (s *CelestiaTestSuite) TestE2ENightlyLatency() {
	t := s.T()

	// Skip in short mode since this is a long-running test
	if testing.Short() {
		t.Skip("skipping nightly latency test in short mode")
	}

	ctx := context.Background()

	// Build and start the celestia chain
	cfg := dockerchain.DefaultConfig(s.client, s.network)
	celestia, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	s.Require().NoError(err, "failed to build celestia chain")

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start celestia chain")

	// Cleanup resources when the test is done
	t.Cleanup(func() {
		if err := celestia.Remove(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	// Verify the chain is running
	height, err := celestia.Height(ctx)
	s.Require().NoError(err, "failed to get chain height")
	s.Require().Greater(height, int64(0), "chain height is zero")

	t.Logf("Starting nightly latency test on chain at height %d", height)

	// Setup transaction client using the chain
	node := celestia.GetNodes()[0].(*tastoradockertypes.ChainNode)
	txClient, err := dockerchain.SetupTxClient(ctx, node, cfg)
	s.Require().NoError(err, "failed to setup tx client")

	// Create namespace for test blobs
	ns := testfactory.RandomBlobNamespace()

	t.Logf("Running nightly latency test with config:")
	t.Logf("  Duration: %v", nightlyTestDuration)
	t.Logf("  Submission Delay: %v", submissionDelay)
	t.Logf("  Latency Threshold: %v", latencyThreshold)
	t.Logf("  Blob Size Range: %d-%d bytes", minBlobSize, blobSize)
	t.Logf("  Account: %s", txClient.DefaultAccountName())

	// Create latency monitor config
	monitorConfig := latency.Config{
		Endpoint:         "", // maybe remove?
		KeyringDir:       "", // maybe remove?
		AccountName:      txClient.DefaultAccountName(),
		BlobSize:         blobSize,
		MinBlobSize:      minBlobSize,
		NamespaceStr:     "", // maybe i could remove
		SubmissionDelay:  submissionDelay,
		TestDuration:     nightlyTestDuration,
		LatencyThreshold: latencyThreshold,
		DisableMetrics:   false,
	}

	// Create and setup the latency monitor
	monitor, err := latency.NewMonitor(monitorConfig)
	s.Require().NoError(err, "failed to create latency monitor")
	defer monitor.Cleanup()

	// Setup the monitor with the existing tx client and namespace
	err = monitor.SetupWithExistingClient(txClient, ns)
	s.Require().NoError(err, "failed to setup monitor with existing client")

	startTime := time.Now()
	t.Logf("Starting latency test at %v", startTime)

	// Run the latency test
	testCtx, cancel := context.WithTimeout(ctx, nightlyTestDuration+30*time.Second) // Add buffer for cleanup
	defer cancel()

	results, err := monitor.Run(testCtx) // TODO: also think more about results and calculations
	// Expect context deadline exceeded as normal completion
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		s.Require().NoError(err, "latency test failed unexpectedly")
	}

	// Wait a bit for pending confirmations
	t.Logf("Waiting for pending transaction confirmations...")
	time.Sleep(10 * time.Second)

	endTime := time.Now()
	actualDuration := endTime.Sub(startTime)
	t.Logf("Test completed after %v", actualDuration)

	// Calculate and validate statistics
	stats := monitor.CalculateStatistics()
	s.logTestResults(t, stats)

	// Validate reliability requirements
	s.validateReliabilityRequirements(t, monitor, stats)

	// Additional validations for nightly tests
	s.validateLatencyMetrics(t, stats, nightlyTestDuration)
}

// logTestResults logs the test results in a structured format
func (s *CelestiaTestSuite) logTestResults(t *testing.T, stats latency.Statistics) {
	t.Logf("=== Nightly Latency Test Results ===")
	t.Logf("Total Transactions: %d", stats.TotalTransactions)
	t.Logf("Successful: %d (%.2f%%)", stats.SuccessfulTxs, stats.SuccessRate)
	t.Logf("Failed: %d", stats.FailedTxs)

	if stats.SuccessfulTxs > 0 {
		t.Logf("Average Latency: %v", stats.AverageLatency)
		t.Logf("Std Deviation: %v", stats.StandardDeviation)
	}
}

// validateReliabilityRequirements validates the 100% success rate requirement - KEY ASSERTION
func (s *CelestiaTestSuite) validateReliabilityRequirements(t *testing.T, monitor *latency.Monitor, stats latency.Statistics) {
	// Use the monitor's built-in validation
	err := s.ValidateReliability(monitor.GetResults(), latencyThreshold)
	if err != nil {
		t.Errorf("reliability checks failed: %v", err)
		t.Errorf("success rate: %.2f%% (required: 100%%)", stats.SuccessRate)
		t.Errorf("failed transactions: %d out of %d", stats.FailedTxs, stats.TotalTransactions)

		// Log failed transaction details for debugging
		results := monitor.GetResults()
		for i, result := range results {
			if result.Failed {
				t.Errorf("Failed TX %d: %s - %s", i+1, result.TxHash, result.ErrorMsg)
			}
		}

		// This will cause the test to fail
		s.Require().NoError(err, "Nightly latency test failed reliability requirements")
	}

	t.Logf("reliability checks passed: 100%% success rate achieved and confirmation threshold not exceeded")
}

// validateLatencyMetrics performs additional validation on latency metrics
func (s *CelestiaTestSuite) validateLatencyMetrics(t *testing.T, stats latency.Statistics, testDuration time.Duration) {
	// Validate that we have a reasonable number of transactions for the test duration
	expectedMinTxs := int(testDuration / submissionDelay)
	if stats.TotalTransactions < expectedMinTxs/2 { // Allow 50% tolerance
		t.Errorf("Too few transactions submitted: got %d, expected at least %d",
			stats.TotalTransactions, expectedMinTxs/2)
		s.Require().GreaterOrEqual(stats.TotalTransactions, expectedMinTxs/2, "Insufficient transactions submitted during test")
	}

	// Validate latency metrics are reasonable
	if stats.SuccessfulTxs > 0 {
		// Average latency should be reasonable (less than 30 seconds for local testnet)
		if stats.AverageLatency > latencyThreshold {
			t.Errorf("Average latency too high: %v (threshold: %s)", stats.AverageLatency, latencyThreshold)
		}

		t.Logf("Average latency is less than the threshold")
	}
}

// reliability checks should be inside the test itself
func (s *CelestiaTestSuite) ValidateReliability(results []latency.TxResult, latencyThreshold time.Duration) error {
	stats := latency.CalculateStatistics(results, latencyThreshold)

	// Check for 100% success rate requirement
	if stats.SuccessRate < 100.0 {
		return fmt.Errorf("reliability check failed: success rate %.2f%% is below required 100%%", stats.SuccessRate)
	}

	return nil
}

// TODO: also test 100% success rate with variable blob sizes with different submission delays

// TODO: also test when you spam with random delays? 
