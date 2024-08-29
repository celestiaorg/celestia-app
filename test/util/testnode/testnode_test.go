package testnode_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
)

func Test_testnode(t *testing.T) {
	t.Run("testnode can start a network with default chain ID", func(t *testing.T) {
		testnode.NewNetwork(t, testnode.DefaultConfig())
	})
	t.Run("testnode can start a network with a custom chain ID", func(t *testing.T) {
		chainID := "custom-chain-id"
		config := testnode.DefaultConfig().WithChainID(chainID)
		testnode.NewNetwork(t, config)
	})
}
