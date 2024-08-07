package cmd

import (
	"context"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/node/utils"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	currentAppVersion := uint64(1)
	apps := utils.GetApps()
	multiplexer := utils.NewMultiplexer(currentAppVersion, apps)
	config := testnode.DefaultConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cctx, err := utils.StartNode(ctx, config, multiplexer)
	require.NoError(t, err)
	fmt.Printf("chainID %v\n", cctx.ChainID)

	latestHeight, err := cctx.LatestHeight()
	require.NoError(t, err)
	fmt.Printf("latestHeight %v\n", latestHeight)

	err = cctx.WaitForNextBlock()
	require.NoError(t, err)
}
