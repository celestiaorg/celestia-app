package docker_e2e

import (
	"context"
	celestiadockertypes "github.com/celestiaorg/tastora/framework/docker"
	addressutil "github.com/celestiaorg/tastora/framework/testutil/address"
	"github.com/celestiaorg/tastora/framework/testutil/toml"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	"testing"
	"time"
)

const (
	blocksToProduce            = 30
	stateSyncTrustHeightOffset = 5
	stateSyncTimeout           = 10 * time.Minute
)

// validatorStateSyncAppOverrides generates a TOML configuration to enable state sync and snapshot functionality for validators.
func validatorStateSyncAppOverrides() toml.Toml {
	overrides := make(toml.Toml)
	snapshot := make(toml.Toml)
	snapshot["interval"] = 5
	snapshot["keep_recent"] = 2
	overrides["snapshot"] = snapshot

	stateSync := make(toml.Toml)
	stateSync["snapshot-interval"] = 5
	stateSync["snapshot-keep-recent"] = 2
	overrides["state-sync"] = stateSync
	return overrides
}

func (s *CelestiaTestSuite) TestStateSync() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()
	chainProvider := s.CreateDockerProvider(func(config *celestiadockertypes.Config) {
		numVals := 3
		// require at least 2 validators for state sync to work.
		config.ChainConfig.NumValidators = &numVals
		config.ChainConfig.ConfigFileOverrides = map[string]any{
			// enable state-sync and snapshots on validators.
			"config/app.toml": validatorStateSyncAppOverrides(),
		}
	})

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

	// verify the chain is running
	height, err := celestia.Height(ctx)
	s.Require().NoError(err, "failed to get chain height")
	s.Require().Greater(height, int64(0), "chain height is zero")

	s.CreateTxSim(ctx, celestia)

	allNodes := celestia.GetNodes()

	nodeClient, err := allNodes[0].GetRPCClient()
	s.Require().NoError(err)

	initialHeight := int64(0)

	// Use a ticker to periodically check for the initial height
	heightTicker := time.NewTicker(1 * time.Second)
	defer heightTicker.Stop()

	heightTimeoutCtx, heightCancel := context.WithTimeout(ctx, 30*time.Second) // Wait up to 30 seconds for the first block
	defer heightCancel()

	// Check immediately first, then on ticker intervals
	InitialHeightLoop:
	for {
		status, err := nodeClient.Status(heightTimeoutCtx)
		if err == nil && status.SyncInfo.LatestBlockHeight > 0 {
			initialHeight = status.SyncInfo.LatestBlockHeight
			break InitialHeightLoop
		}

		select {
		case <-heightTicker.C:
			// Continue the loop
		case <-heightTimeoutCtx.Done():
			t.Logf("Timed out waiting for initial height")
			break InitialHeightLoop
		}
	}

	s.Require().Greater(initialHeight, int64(0), "failed to get initial height after timeout or error")
	targetHeight := initialHeight + blocksToProduce
	t.Logf("Successfully reached initial height %d", initialHeight)

	s.Require().NoError(wait.ForBlocks(ctx, int(targetHeight), celestia), "failed to wait for target height")

	t.Logf("Successfully reached target height %d", targetHeight)

	t.Logf("Gathering state sync parameters")
	status, err := nodeClient.Status(ctx)
	s.Require().NoError(err, "failed to get node zero client")

	latestHeight := status.SyncInfo.LatestBlockHeight
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

	overrides := map[string]any{
		"config/config.toml": stateSyncOverrides(trustHeight, trustHash, rpcServers),
	}

	t.Log("Adding state sync node")
	err = celestia.AddNode(ctx, overrides)

	s.Require().NoError(err, "failed to add node")

	allNodes = celestia.GetNodes()
	fullNode := allNodes[len(allNodes)-1]

	s.Require().Equal("fn", fullNode.GetType(), "expected state sync node to be a full node")

	stateSyncClient, err := fullNode.GetRPCClient()
	s.Require().NoError(err)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, stateSyncTimeout)
	defer cancel()

	// Check immediately first, then on ticker intervals
	for {
		status, err := stateSyncClient.Status(timeoutCtx)
		if err != nil {
			t.Logf("Failed to get status from state sync node, retrying...: %v", err)
			select {
			case <-ticker.C:
				continue
			case <-timeoutCtx.Done():
				t.Fatalf("timed out waiting for state sync node to catch up after %v", stateSyncTimeout)
			}
		}

		t.Logf("State sync node status: Height=%d, CatchingUp=%t", status.SyncInfo.LatestBlockHeight, status.SyncInfo.CatchingUp)

		if !status.SyncInfo.CatchingUp && status.SyncInfo.LatestBlockHeight >= latestHeight {
			t.Logf("State sync successful! Node caught up to height %d", status.SyncInfo.LatestBlockHeight)
			break
		}

		select {
		case <-ticker.C:
			// Continue the loop
		case <-timeoutCtx.Done():
			t.Fatalf("timed out waiting for state sync node to catch up after %v", stateSyncTimeout)
		}
	}
}

// stateSyncOverrides returns config overrides which will enable state sync.
func stateSyncOverrides(trustHeight int64, trustHash, rpcServers string) toml.Toml {
	stateSyncConfig := make(toml.Toml)
	stateSyncConfig["enable"] = true
	stateSyncConfig["trust_height"] = trustHeight
	stateSyncConfig["trust_hash"] = trustHash
	stateSyncConfig["rpc_servers"] = rpcServers

	configOverrides := make(toml.Toml)
	configOverrides["statesync"] = stateSyncConfig
	return configOverrides
}
