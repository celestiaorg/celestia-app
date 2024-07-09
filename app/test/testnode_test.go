package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

func Test_testnode(t *testing.T) {
	t.Run("testnode can start a network with default chain ID", func(t *testing.T) {
		testnode.NewNetwork(t, testnode.DefaultConfig())
	})
	t.Run("testnode can start a network with a custom chain ID", func(t *testing.T) {
		chainID := "custom-chain-id"

		// Set the chain ID on genesis. If this isn't done, the default chain ID
		// is used for the validator which results in a "signature verification
		// failed" error.
		genesis := genesis.NewDefaultGenesis().
			WithChainID(chainID).
			WithValidators(genesis.NewDefaultValidator(testnode.DefaultValidatorAccountName)).
			WithConsensusParams(testnode.DefaultConsensusParams())

		config := testnode.DefaultConfig()
		config.WithChainID(chainID)
		config.WithGenesis(genesis)
		testnode.NewNetwork(t, config)
	})
}
