package spoon

import (
	"testing"

	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

var t = coretypes.ABCIPubKeyTypeEd25519

func TestLoadGenState(t *testing.T) {
	// change these if you would like to test the genesis locally
	srcPath := "/home/evan/mamaki-326472.json"
	dstPath := "/home/evan/.testytest/config/genesis.json"
	newstate, kr, err := Fork(srcPath, dstPath, "validator")
	require.NoError(t, err)

	// try to run a testnode w/ the modified genesis
	tmNode, _, cctx, err := testnode.New(
		t,
		testnode.DefaultParams(),
		testnode.DefaultTendermintConfig(),
		false,
		newstate,
		kr,
		"espresso",
	)
	require.NoError(t, err)

	cctx, stopNode, err := testnode.StartNode(tmNode, cctx)
	require.NoError(t, err)

	err = cctx.WaitForBlocks(10)
	require.NoError(t, err)

	stopNode()
}

func TestFork(t *testing.T) {
	srcPath := "/home/evan/fork/fork.json"
	dstPath := "/home/evan/fork/test-genesis.json"
	_, _, err := Fork(srcPath, dstPath)
	require.NoError(t, err)
}
