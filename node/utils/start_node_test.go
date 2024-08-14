package utils_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/node/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmdb "github.com/tendermint/tm-db"
)

func TestStartNode(t *testing.T) {
	dbPath := t.TempDir()
	db, err := tmdb.NewGoLevelDB("application", dbPath)
	require.NoError(t, err)

	multiplexer := utils.NewMultiplexer(db)
	config := utils.GetConfig()

	tempDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cctx, cleanup, err := utils.StartNode(ctx, config, multiplexer, tempDir)
	defer cleanup()
	require.NoError(t, err)
	fmt.Printf("chainID %v\n", cctx.ChainID)

	assert.Eventually(t, func() bool {
		latestHeight, err := cctx.LatestHeight()
		require.NoError(t, err)
		return latestHeight > 5
	}, time.Second*10, time.Second)
}
