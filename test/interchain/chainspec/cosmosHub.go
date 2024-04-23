package chainspec

import (
	"testing"

	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	// TODO: As of this writing, gaia has no official releases with the ICA
	// controller enabled. However, they do have it enabled on `main` so the
	// Docker image below is a custom build from `main`. Replace this with an
	// official release when available (likely >= 16.0.0).
	cosmosDockerRepository = "docker.io/rootulp/gaia"
	cosmosDockerVersion    = "ica-controller"
	cosmosUidGid           = "1025:1025"
)

// GetCosmosHub returns a CosmosChain for the CosmosHub.
func GetCosmosHub(t *testing.T) *cosmos.CosmosChain {
	factory := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{cosmosHub})
	chains, err := factory.Chains(t.Name())
	require.NoError(t, err)
	return chains[0].(*cosmos.CosmosChain)
}

func GetCosmosHub2(t *testing.T) *cosmos.CosmosChain {
	factory := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{cosmosHub2})
	chains, err := factory.Chains(t.Name())
	require.NoError(t, err)
	return chains[0].(*cosmos.CosmosChain)
}

var cosmosHub = &interchaintest.ChainSpec{
	Name: "gaia",
	ChainConfig: ibc.ChainConfig{
		Name:                   "gaia",
		Type:                   "cosmos",
		ChainID:                "gaia",
		Bin:                    "gaiad",
		Bech32Prefix:           "cosmos",
		Denom:                  "uatom",
		GasPrices:              "0.01uatom",
		GasAdjustment:          *gasAdjustment(),
		TrustingPeriod:         "504hours",
		NoHostMount:            false,
		Images:                 cosmosDockerImages(),
		UsingNewGenesisCommand: true,
	},
	NumValidators: numValidators(),
	NumFullNodes:  numFullNodes(),
	GasAdjustment: gasAdjustment(), // the default gas estimation fails to create a client on Cosmos Hub so we need to bump it up.
}

var cosmosHub2 = &interchaintest.ChainSpec{
	Name: "gaia2",
	ChainConfig: ibc.ChainConfig{
		Name:                   "gaia2",
		Type:                   "cosmos",
		ChainID:                "gaia2",
		Bin:                    "gaiad",
		Bech32Prefix:           "cosmos",
		Denom:                  "uatom",
		GasPrices:              "0.01uatom",
		GasAdjustment:          *gasAdjustment(),
		TrustingPeriod:         "504hours",
		NoHostMount:            false,
		Images:                 cosmosDockerImages(),
		UsingNewGenesisCommand: true,
	},
	NumValidators: numValidators(),
	NumFullNodes:  numFullNodes(),
	GasAdjustment: gasAdjustment(), // the default gas estimation fails to create a client on Cosmos Hub so we need to bump it up.
}

func cosmosDockerImages() []ibc.DockerImage {
	return []ibc.DockerImage{
		{
			Repository: cosmosDockerRepository,
			Version:    cosmosDockerVersion,
			UidGid:     cosmosUidGid,
		},
	}
}
