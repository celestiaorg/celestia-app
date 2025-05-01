package docker_e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/chatton/interchaintest/chain/cosmos"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func (s *CelestiaTestSuite) TestE2ESimple() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	celestia, err := s.CreateCelestiaChain("v4.0.0-rc1", "4")
	s.Require().NoError(err)

	// Start the chain
	ctx := context.Background()
	err = celestia.Start(ctx)
	require.NoError(t, err)

	// Cleanup resources when the test is done
	t.Cleanup(func() {
		if err := celestia.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	// Verify the chain is running
	height, err := celestia.Height(ctx)
	require.NoError(t, err)
	require.Greater(t, height, int64(0))

	// Get the validators
	cosmosChain, ok := celestia.(*cosmos.Chain)
	require.True(t, ok, "expected celestia to be a cosmos.Chain")

	s.CreateTxSim(ctx, "v4.0.0-rc1", cosmosChain)

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
			t.Logf("Timed out waiting for %d transactions", requiredTxs)
			t.Failed()
		}
	}
}
