package cmd

import (
	"context"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/node/utils"
	"github.com/stretchr/testify/assert"
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
	cctx, err := utils.StartNode(ctx, config, multiplexer, tempDir)
	require.NoError(t, err)
	fmt.Printf("chainID %v\n", cctx.ChainID)

	latestHeight, err := cctx.LatestHeight()
	require.NoError(t, err)
	fmt.Printf("latestHeight %v\n", latestHeight)
	assert.Greater(t, latestHeight, int64(0))
}
