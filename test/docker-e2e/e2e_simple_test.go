package docker_e2e

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v5/test/util/testnode"
)

func (s *CelestiaTestSuite) TestE2ESimple() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()
	chainProvider := s.CreateDockerProvider()

	celestia, err := chainProvider.GetChain(ctx)
	s.Require().NoError(err, "failed to get chain")

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	// Cleanup resources when the test is done
	t.Cleanup(func() {
		if err := celestia.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	// Verify the chain is running
	height, err := celestia.Height(ctx)
	s.Require().NoError(err, "failed to get chain height")
	s.Require().Greater(height, int64(0), "chain height is zero")

	s.CreateTxSim(ctx, celestia)

	pollCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	const requiredTxs = 10
	const pollInterval = 5 * time.Second

	// periodically check for transactions until timeout or required transactions are found
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check for transactions
			headers, err := testnode.ReadBlockchainHeaders(ctx, celestia.GetHostRPCAddress())
			if err != nil {
				t.Logf("Error reading blockchain headers: %v", err)
				continue
			}

			totalTxs := 0
			for _, blockMeta := range headers {
				totalTxs += blockMeta.NumTxs
			}

			t.Logf("Current transaction count: %d", totalTxs)

			if totalTxs >= requiredTxs {
				t.Logf("Found %d transactions, continuing with test", totalTxs)
				return
			}
		case <-pollCtx.Done():
			t.Fatalf("Timed out waiting for %d transactions", requiredTxs)
		}
	}
}
