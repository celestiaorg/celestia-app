package chainspec

import (
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	cosmosDockerRepository = "ghcr.io/strangelove-ventures/heighliner/gaia"
	cosmosDockerVersion    = "v17.2.0"
	cosmosUidGid           = "1025:1025"
)

// GetCosmosHub returns a CosmosChain for the CosmosHub.
func GetCosmosHub(t *testing.T) *cosmos.CosmosChain {
	factory := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{cosmosHub})
	chains, err := factory.Chains(t.Name())
	require.NoError(t, err)
	return chains[0].(*cosmos.CosmosChain)
}

var cosmosHub = &interchaintest.ChainSpec{
	Name: "gaia",
	ChainConfig: ibc.ChainConfig{
		Name:           "gaia",
		Type:           "cosmos",
		ChainID:        "gaia",
		Bin:            "gaiad",
		Bech32Prefix:   "cosmos",
		Denom:          "uatom",
		GasPrices:      "0.01uatom",
		GasAdjustment:  *gasAdjustment(),
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
			UidGid:     cosmosUidGid,
		},
	}
}
