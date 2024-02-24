package interchaintest_test

import (
	"testing"

	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

var celestiaSpec = &interchaintest.ChainSpec{
	Name: "celestia",
	ChainConfig: ibc.ChainConfig{
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
	},
	Version: "v1.6.0",
}
var cosmosSpec = &interchaintest.ChainSpec{
	Name:        "gaia",
	ChainConfig: cosmos.NewCosmosHeighlinerChainConfig("gaia", "gaiad", "cosmos", "uatom", "0.01uatom", 1.3, "504h", false),
	Version:     "v14.1.0",
}

// getChains returns two chains for testing: celestia and gaia.
func getChains(t *testing.T) (celestia *cosmos.CosmosChain, gaia *cosmos.CosmosChain) {
	chainSpecs := []*interchaintest.ChainSpec{celestiaSpec, cosmosSpec}
	chainFactory := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), chainSpecs)
	chains, err := chainFactory.Chains(t.Name())
	require.NoError(t, err)
	return chains[0].(*cosmos.CosmosChain), chains[1].(*cosmos.CosmosChain)
}
