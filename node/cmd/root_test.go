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
	tempDir := t.TempDir()
	fmt.Printf("dbPath %v\n", config.TmConfig.DBPath)
	fmt.Printf("dbDir %v\n", config.TmConfig.DBDir())
	fmt.Printf("tempDir %v\n", tempDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cctx, err := utils.StartNode(ctx, config, multiplexer, tempDir)
	require.NoError(t, err)
	fmt.Printf("chainID %v\n", cctx.ChainID)

	latestHeight, err := cctx.LatestHeight()
	require.NoError(t, err)
	fmt.Printf("latestHeight %v\n", latestHeight)

	err = cctx.WaitForNextBlock()
	require.NoError(t, err)
}
