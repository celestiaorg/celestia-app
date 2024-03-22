package interchaintest_test

import (
	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
	"github.com/strangelove-ventures/interchaintest/v6/testutil"
)

const (
	celestiaDockerRepository = "ghcr.io/celestiaorg/celestia-app"
	celestiaDockerTag        = "pr-3182"
)

var celestiaSpec = &interchaintest.ChainSpec{
	Name: "celestia",
	ChainConfig: ibc.ChainConfig{
		Type:                "cosmos",
		Name:                "celestia-app",
		ChainID:             "celestia",
		Bin:                 "celestia-appd",
		Bech32Prefix:        "celestia",
		Denom:               "utia",
		GasPrices:           "0.002utia",
		GasAdjustment:       1.5,
		TrustingPeriod:      "336hours",
		Images:              celestiaDockerImages(),
		ConfigFileOverrides: celestiaConfigFileOverrides(),
	},
	NumValidators: numValidators(),
	NumFullNodes:  numFullNodes(),
}

func celestiaDockerImages() []ibc.DockerImage {
	return []ibc.DockerImage{
		{
			Repository: celestiaDockerRepository,
			Version:    celestiaDockerTag,
			UidGid:     "10001:10001",
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
