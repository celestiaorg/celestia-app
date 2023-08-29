package spoon_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/cmd/spoon"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"
)

// no I need to create a genesis option that spoons!
func replaceGenesis(newstate map[string]json.RawMessage) testnode.GenesisOption {
	return func(_ map[string]json.RawMessage) map[string]json.RawMessage {
		return newstate
	}
}

func TestFork(t *testing.T) {
	// change these if you would like to test the genesis locally
	srcPath := "/home/evan/fork/fork.json"
	dstPath := "/home/evan/fork/new_genesis.json"
	chainid := "taco"
	// create a few accounts and give them funds, notable the validator
	newstate, kr, err := spoon.Spoon(srcPath, dstPath, chainid, "validator")
	require.NoError(t, err)
	fmt.Println("creating testnode")

	cfg := testnode.DefaultConfig().
		WithGenesisOptions(replaceGenesis(newstate)).
		WithChainID(chainid).
		WithKeyring(kr)

	// try to run a testnode w/ the modified genesis
	cctx, _, _ := testnode.NewNetwork(
		t,
		cfg,
	)

	cctx.Keyring = kr
	require.NoError(t, err)

	_, err = cctx.WaitForHeight(40)
	require.NoError(t, err)
}

func TestNoRun(t *testing.T) {
	srcPath := "/home/evan/fork/fork.json"
	dstPath := "/home/evan/fork/test-genesis.json"
	_, _, err := spoon.Spoon(srcPath, dstPath, "test")
	require.NoError(t, err)
}
