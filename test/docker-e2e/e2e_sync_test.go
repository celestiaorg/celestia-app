package docker_e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/chatton/interchaintest"
	"github.com/chatton/interchaintest/chain/cosmos"
	"github.com/chatton/interchaintest/ibc"
	maps "github.com/chatton/interchaintest/testutil/maps"
	"github.com/chatton/interchaintest/testutil/toml"
	"github.com/chatton/interchaintest/testutil/wait"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

func TestCelestiaChainStateSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	const (
		blocksToProduce            = 30
		stateSyncTrustHeightOffset = 5
		stateSyncTimeout           = 5 * time.Minute
	)

	// Create a new logger for the test
	logger := zaptest.NewLogger(t)

	// Setup Docker client and network
	client, network := interchaintest.DockerSetup(t)

	// Define the number of validators
	numValidators := 4
	numFullNodes := 0

	enc := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)
	// Create a single Celestia chain directly
	celestia, err := interchaintest.NewChain(logger, t.Name(), client, network, &interchaintest.ChainSpec{
		Name:          "celestia",
		ChainName:     "celestia",
		Version:       "v4.0.0-rc1",
		NumValidators: &numValidators,
		NumFullNodes:  &numFullNodes,
		Config: ibc.Config{
			ModifyGenesis: func(config ibc.Config, bytes []byte) ([]byte, error) {
				return maps.SetField(bytes, "consensus.params.version.app", "4")
			},
			EncodingConfig:      &enc,
			AdditionalStartArgs: []string{"--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099"},
			Type:                "cosmos",
			ChainID:             "celestia",
			Images: []ibc.DockerImage{
				{
					Repository: "ghcr.io/celestiaorg/celestia-app-multiplexer",
					Version:    "v4.0.0-rc1",
					UIDGID:     "10001:10001",
				},
			},
			Bin:           "celestia-appd",
			Bech32Prefix:  "celestia",
			Denom:         "utia",
			GasPrices:     "0.025utia",
			GasAdjustment: 1.3,
		},
	})
	require.NoError(t, err)

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

	// Verify we have the expected number of validators
	require.Equal(t, numValidators, len(cosmosChain.Validators))

	t.Logf("Successfully started Celestia chain with %d validators", numValidators)

	createTxSim(t, err, ctx, client, network, logger, cosmosChain)

	nodeClient := cosmosChain.Nodes()[0].Client

	initialHeight := int64(0)

	// Use a ticker to periodically check for the initial height
	heightTicker := time.NewTicker(1 * time.Second)
	defer heightTicker.Stop()

	heightTimeoutCtx, heightCancel := context.WithTimeout(ctx, 30*time.Second) // Wait up to 30 seconds for the first block
	defer heightCancel()

	// Check immediately first, then on ticker intervals
	for {
		status, err := nodeClient.Status(heightTimeoutCtx)
		if err == nil && status.SyncInfo.LatestBlockHeight > 0 {
			initialHeight = status.SyncInfo.LatestBlockHeight
			break
		}

		select {
		case <-heightTicker.C:
			// Continue the loop
		case <-heightTimeoutCtx.Done():
			t.Logf("Timed out waiting for initial height")
			break
		}
	}

	require.Greater(t, initialHeight, int64(0), "failed to get initial height")
	targetHeight := initialHeight + blocksToProduce
	t.Logf("Successfully reached initial height %d", initialHeight)

	require.NoError(t, wait.ForBlocks(ctx, int(targetHeight), celestia), "failed to wait for target height")

	t.Logf("Successfully reached target height %d", targetHeight)

	t.Logf("Gathering state sync parameters")
	status, err := nodeClient.Status(ctx)
	require.NoError(t, err, "failed to get node zero client")

	latestHeight := status.SyncInfo.LatestBlockHeight
	trustHeight := latestHeight - stateSyncTrustHeightOffset
	require.Greaterf(t, trustHeight, int64(0), "calculated trust height %d is too low (latest height: %d)", trustHeight, latestHeight)

	trustBlock, err := nodeClient.Block(ctx, &trustHeight)
	require.NoError(t, err, "failed to get block at trust height %d", trustHeight)

	trustHash := trustBlock.BlockID.Hash.String()
	rpcServers := cosmosChain.Nodes().RPCString(ctx)

	t.Logf("Trust height: %d", trustHeight)
	t.Logf("Trust hash: %s", trustHash)
	t.Logf("RPC servers: %s", rpcServers)

	overrides := map[string]any{}
	configOverrides := make(toml.Toml)
	configOverrides["state_sync.enable"] = true
	configOverrides["state_sync.trust_height"] = trustHeight
	configOverrides["state_sync.trust_hash"] = trustHash
	configOverrides["state_sync.rpc_servers"] = rpcServers

	overrides["config/config.toml"] = configOverrides
	err = cosmosChain.AddFullNodes(ctx, overrides, 1)
	require.NoError(t, err, "failed to add node")

	stateSyncClient := cosmosChain.FullNodes[0].Client

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
