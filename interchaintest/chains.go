package interchaintest_test

import (
	"testing"

	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/chain/cosmos"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// getChains returns two chains for testing: celestia and stride.
func getChains(t *testing.T) (celestia *cosmos.CosmosChain, stride *cosmos.CosmosChain) {
	chainSpecs := []*interchaintest.ChainSpec{celestiaSpec, strideSpec}
	chainFactory := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), chainSpecs)
	chains, err := chainFactory.Chains(t.Name())
	require.NoError(t, err)
	return chains[0].(*cosmos.CosmosChain), chains[1].(*cosmos.CosmosChain)
}

func numValidators() *int {
	numValidators := 1
	return &numValidators
}

func numFullNodes() *int {
	numValidators := 0
	return &numValidators
}
