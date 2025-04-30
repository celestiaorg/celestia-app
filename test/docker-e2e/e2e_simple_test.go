package docker_e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/chatton/interchaintest"
	"github.com/chatton/interchaintest/chain/cosmos"
	"github.com/chatton/interchaintest/ibc"
	maps "github.com/chatton/interchaintest/testutil/maps"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

func TestE2ESimple(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

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

	// Wait for a short time to allow txsim to start
	time.Sleep(10 * time.Second)

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
			blockchain, err := testnode.ReadBlockchainHeaders(ctx, celestia.GetHostRPCAddress())
			if err != nil {
				t.Logf("Error reading blockchain headers: %v", err)
				continue
			}

			totalTxs := 0
			for _, blockMeta := range blockchain {
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
