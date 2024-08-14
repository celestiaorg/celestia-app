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
	config := utils.GetConfig()
	fmt.Printf("config.TMConfig.DBDir(): %v\n", config.TmConfig.DBDir())
	db, err := tmdb.NewGoLevelDB("application", config.TmConfig.DBDir())
	require.NoError(t, err)

	multiplexer := utils.NewMultiplexer(db)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cctx, cleanup, err := utils.StartNode(ctx, config, multiplexer)
	defer cleanup()
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		latestHeight, err := cctx.LatestHeight()
		require.NoError(t, err)
		return latestHeight > 10
	}, time.Second*10, time.Second)
}
