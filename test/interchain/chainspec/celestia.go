package chainspec

import (
	"testing"

	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
	"github.com/strangelove-ventures/interchaintest/v6/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	celestiaDockerRepository = "ghcr.io/celestiaorg/celestia-app"
	celestiaDockerTag        = "pr-3182"
	celestiaUidGid           = "10001:10001"
)

// GetCelestia returns a CosmosChain for Celestia.
func GetCelestia(t *testing.T) *cosmos.CosmosChain {
	factory := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{celestia})
	chains, err := factory.Chains(t.Name())
	require.NoError(t, err)
	return chains[0].(*cosmos.CosmosChain)
}

var celestia = &interchaintest.ChainSpec{
	Name: "celestia",
	ChainConfig: ibc.ChainConfig{
		Type:                "cosmos",
		Name:                "celestia-app",
		ChainID:             "celestia",
		Bin:                 "celestia-appd",
		Bech32Prefix:        "celestia",
		Denom:               "utia",
		GasPrices:           "0.002utia",
		GasAdjustment:       *gasAdjustment(),
		TrustingPeriod:      "336hours",
		Images:              celestiaDockerImages(),
		ConfigFileOverrides: celestiaConfigFileOverrides(),
	},
	NumValidators: numValidators(),
	NumFullNodes:  numFullNodes(),
	GasAdjustment: gasAdjustment(),
}

func celestiaDockerImages() []ibc.DockerImage {
	return []ibc.DockerImage{
		{
			Repository: celestiaDockerRepository,
			Version:    celestiaDockerTag,
			UidGid:     celestiaUidGid,
		},
	}
}

func celestiaConfigFileOverrides() map[string]any {
	txIndex := make(testutil.Toml)
	txIndex["indexer"] = "kv"

	storage := make(testutil.Toml)
	storage["discard_abci_responses"] = false

	configToml := make(testutil.Toml)
	configToml["tx_index"] = txIndex
	configToml["storage"] = storage

	result := make(map[string]any)
	result["config/config.toml"] = configToml
	return result
}
