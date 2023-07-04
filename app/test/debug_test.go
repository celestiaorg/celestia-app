package app_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
)

func TestDebug(t *testing.T) {
	// tmcfg := testnode.DefaultTendermintConfig()
	// tmcfg.Genesis = "/home/evan/fork/new/genesis.json"

	// // cfg := testnode.DefaultConfig().
	// // 	WithTendermintConfig(tmcfg)

	// // cctx, _, _ := testnode.NewNetwork(t, cfg)

	// fmt.Println(cctx.ChainID)

	gd, err := types.GenesisDocFromFile("/home/evan/go/src/github.com/celestiaorg/networks/mocha-3/genesis.json")
	require.NoError(t, err)
	fmt.Println(gd.ChainID)
}
