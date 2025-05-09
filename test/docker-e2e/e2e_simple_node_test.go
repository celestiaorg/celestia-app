package docker_e2e

import (
	"context"
	"github.com/chatton/celestia-test/framework/testutil/wait"
	"testing"
	"time"
)

func (s *CelestiaTestSuite) TestE2EBridgeNode() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()
	provider := s.CreateDockerProvider("4")

	celestia, err := provider.GetChain(ctx)
	s.Require().NoError(err)

	err = celestia.Start(ctx)
	s.Require().NoError(err)

	// Cleanup resources when the test is done
	t.Cleanup(func() {
		if err := celestia.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	// Verify the chain is running
	height, err := celestia.Height(ctx)
	s.Require().NoError(err)
	s.Require().Greater(height, int64(0))

	s.CreateTxSim(ctx, celestia)

	// wait for some blocks to ensure the bridge node can sync up.
	s.Require().NoError(wait.ForBlocks(ctx, 10, celestia))

	chainNode := celestia.GetNodes()[0]
	genesisHash := s.getGenesisHash(ctx, chainNode)
	s.Require().NotEmpty(genesisHash, "genesis hash is empty")

	bridgeNode, err := provider.GetNode(ctx, "bridge")
	s.Require().NoError(err)

	hostname, err := chainNode.GetInternalHostName(ctx)
	s.Require().NoError(err)

	err = bridgeNode.Start(ctx, hostname, genesisHash)
	s.Require().NoError(err)
	// Cleanup resources when the test is done
	t.Cleanup(func() {
		if err := bridgeNode.Stop(ctx); err != nil {
			t.Logf("Error stopping bridge node: %v", err)
		}
	})

	celestiaHeight, err := celestia.Height(ctx)
	s.Require().NoError(err)

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			t.Fatalf("Timed out waiting for bridge node to reach height %d: %v", celestiaHeight, waitCtx.Err())
		case <-ticker.C:
			bridgeHeader, err := bridgeNode.GetHeader(ctx, uint64(celestiaHeight))
			if err == nil {
				t.Logf("Bridge header height: %d", bridgeHeader.Height)
				if bridgeHeader.Height >= uint64(celestiaHeight) {
					return // test passed.
				}
			}
			t.Logf("Bridge not yet synced to height %d: %v", celestiaHeight, err)
		}
	}
}
