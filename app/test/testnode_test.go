package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

func Test_testnode(t *testing.T) {
	t.Run("testnode can start a network with default config", func(t *testing.T) {
		testnode.NewNetwork(t, testnode.DefaultConfig())
	})
	t.Run("testnode can start a network with a custom chain ID", func(t *testing.T) {
		config := testnode.DefaultConfig()
		config.Genesis.ChainID = "foo"
		testnode.NewNetwork(t, config)
	})
}
