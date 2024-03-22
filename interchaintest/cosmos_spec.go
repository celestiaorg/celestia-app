package interchaintest_test

import (
	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
)

const (
	cosmosDockerRepository = "ghcr.io/strangelove-ventures/heighliner/gaia"
	cosmosDockerVersion    = "v14.2.0"
)

var cosmosSpec = &interchaintest.ChainSpec{
	Name: "gaia",
	ChainConfig: ibc.ChainConfig{
		Type:           "cosmos",
		Name:           "gaia",
		ChainID:        "cosmoshub-4",
		Bin:            "gaiad",
		Bech32Prefix:   "cosmos",
		Denom:          "uatom",
		GasPrices:      "0.01uatom",
		GasAdjustment:  1.3,
		TrustingPeriod: "504hours",
		NoHostMount:    false,
		Images:         cosmosDockerImages(),
	},
	NumValidators: numValidators(),
	NumFullNodes:  numFullNodes(),
}

func cosmosDockerImages() []ibc.DockerImage {
	return []ibc.DockerImage{
		{
			Repository: cosmosDockerRepository,
			Version:    cosmosDockerVersion,
			UidGid:     "1025:1025",
		},
	}
}
