package spoon

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"
)

func TestLoadGenState(t *testing.T) {
	// change these if you would like to test the genesis locally
	srcPath := "/home/evan/fork/fork.json"
	dstPath := "/home/evan/.celestia-app/config/g3.json"
	chainid := "test"
	newstate, kr, err := Spoon(srcPath, dstPath, chainid, "validator")
	require.NoError(t, err)
	fmt.Println("creating testnode")
	// try to run a testnode w/ the modified genesis
	tmNode, app, cctx, err := testnode.New(
		t,
		testnode.DefaultParams(),
		testnode.DefaultTendermintConfig(),
		false,
		newstate,
		kr,
		chainid,
	)
	require.NoError(t, err)

	cctx, stopNode, err := testnode.StartNode(tmNode, cctx)
	require.NoError(t, err)

	appCfg := testnode.DefaultAppConfig()
	fmt.Println("startinggrc")
	cctx, cleanupGRPC, err := testnode.StartGRPCServer(app, appCfg, cctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		t.Log("tearing down testnode")
		require.NoError(t, stopNode())
		require.NoError(t, cleanupGRPC())
	})

	fmt.Println("waiting for block")
	err = cctx.WaitForBlocks(10)
	require.NoError(t, err)
	fmt.Println("stoping")
	stopNode()
}

func TestFork(t *testing.T) {
	srcPath := "/home/evan/fork/fork.json"
	dstPath := "/home/evan/fork/test-genesis.json"
	_, _, err := Spoon(srcPath, dstPath, "test")
	require.NoError(t, err)
}
