package cmd

import (
	"context"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/node/utils"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	currentAppVersion := uint64(1)
	apps := utils.GetApps()
	multiplexer := utils.NewMultiplexer(currentAppVersion, apps)

	config := utils.GetConfig()
	fmt.Printf("rootDir %v\n", config.TmConfig.RootDir)
	fmt.Printf("dbPath %v\n", config.TmConfig.DBPath)
	fmt.Printf("dbDir %v\n", config.TmConfig.DBDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cctx, err := utils.StartNode(ctx, config, multiplexer)
	require.NoError(t, err)
	fmt.Printf("chainID %v\n", cctx.ChainID)

	latestHeight, err := cctx.LatestHeight()
	require.NoError(t, err)
	fmt.Printf("latestHeight %v\n", latestHeight)

	cctx.WaitForNextBlock()

	latestHeight, err = cctx.LatestHeight()
	require.NoError(t, err)
	fmt.Printf("latestHeight %v\n", latestHeight)
}
