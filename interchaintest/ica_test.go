package interchaintest_test

import (
	"context"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
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

	ctx := context.Background()
	client, network := interchaintest.DockerSetup(t)
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)
	celestiaChainConfig := ibc.ChainConfig{
		Type:           "cosmos",
		Name:           "celestia-app",
		ChainID:        "celestia",
		Images:         []ibc.DockerImage{{Repository: "ghcr.io/celestiaorg/celestia-app", Version: "v1.6.0", UidGid: "10001:10001"}},
		Bin:            "celestia-appd",
		Bech32Prefix:   "celestia",
		Denom:          "utia",
		GasPrices:      "0.002utia",
		GasAdjustment:  1.5,
		TrustingPeriod: "336hours",
	}
	celestiaChainSpec := &interchaintest.ChainSpec{
		Name:        "celestia",
		ChainConfig: celestiaChainConfig,
		Version:     "v1.6.0",
	}
	cosmosChainConfig := cosmos.NewCosmosHeighlinerChainConfig("gaia", "gaiad", "cosmos", "uatom", "0.01uatom", 1.3, "504h", false)
	cosmosChainSpec := &interchaintest.ChainSpec{
		Name:        "gaia",
		ChainConfig: cosmosChainConfig,
		Version:     "v14.1.0",
	}

	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		celestiaChainSpec,
		cosmosChainSpec,
	})
	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)
	chain1, chain2 := chains[0].(*cosmos.CosmosChain), chains[1].(*cosmos.CosmosChain)

	relayer := interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t),
		relayer.RelayerOptionExtraStartFlags{Flags: []string{"-p", "events", "-b", "100"}},
	).Build(t, client, network)
	ic := interchaintest.NewInterchain().
		AddChain(chain1).
		AddChain(chain2).
		AddRelayer(relayer, relayerName).
		AddLink(interchaintest.InterchainLink{
			Chain1:  chain1,
			Chain2:  chain2,
			Relayer: relayer,
			Path:    path,
		})
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: true,
	}))
}
