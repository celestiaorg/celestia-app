package main

import (
	"testing"

	"github.com/celestiaorg/celestia-app/test/util/genesis"
)

func Test(t *testing.T) {
	// create and save a genesis file
	val := genesis.NewDefaultValidator("test")
	// create a validator that doesn't have enough stake to require a signature
	// to produce a block for testing purposes
	val.Stake = 1_000_000_000_000

	g := genesis.NewDefaultGenesis().WithValidators()

	// gDoc, err := g.Export()
	// require.NoError(t, err)

	// create and save a few gentxs

	// run gentest and point to the new genesis file and gentxs
}
