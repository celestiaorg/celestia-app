package cmd

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/node/utils"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	currentAppVersion := uint64(1)
	apps := utils.GetApps()
	multiplexer := utils.NewMultiplexer(currentAppVersion, apps)
	config := utils.GetConfig()

	tempDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cctx, cleanup, err := utils.StartNode(ctx, config, multiplexer, tempDir)
	defer cleanup()
	require.NoError(t, err)
	fmt.Printf("chainID %v\n", cctx.ChainID)

	require.Eventually(t, func() bool {
		latestHeight, err := cctx.LatestHeight()
		require.NoError(t, err)
		return latestHeight > 0
	}, time.Second*10, time.Second)
}
