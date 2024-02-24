package interchaintest_test

import (
	"context"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
	"github.com/strangelove-ventures/interchaintest/v6/relayer"
	"github.com/strangelove-ventures/interchaintest/v6/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const relayerName = "relayer"
const path = "test-path"

// TestICA verifies that Interchain Accounts work as expected.
func TestICA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestICA in short mode.")
	}

	client, network := interchaintest.DockerSetup(t)
	celestia, gaia := getChains(t)

	relayer := interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t),
		relayer.RelayerOptionExtraStartFlags{Flags: []string{"-p", "events", "-b", "100"}},
	).Build(t, client, network)

	ic := interchaintest.NewInterchain().
		AddChain(celestia).
		AddChain(gaia).
		AddRelayer(relayer, relayerName).
		AddLink(interchaintest.InterchainLink{
			Chain1:  celestia,
			Chain2:  gaia,
			Relayer: relayer,
			Path:    path,
		})

	ctx := context.Background()
	rep := testreporter.NewNopReporter().RelayerExecReporter(t)
	// Build the interchain
	err := ic.Build(ctx, rep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: true,
	})
	require.NoError(t, err)
}
