package utils_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/multiplexer/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmdb "github.com/tendermint/tm-db"
)

func TestStartNode(t *testing.T) {
	config := utils.GetConfig()
	fmt.Printf("Deleting root dir: %v\n", config.TmConfig.RootDir)
	os.RemoveAll(config.TmConfig.RootDir)

	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	dbPath := filepath.Join(config.TmConfig.RootDir, "data")
	db, err := tmdb.NewGoLevelDB("application", dbPath)
	require.NoError(t, err)
	multiplexer := utils.NewMultiplexer(logger, db)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cctx, cleanup, err := utils.StartNode(ctx, config, multiplexer)
	defer cleanup()
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		latestHeight, err := cctx.LatestHeight()
		require.NoError(t, err)
		return latestHeight > 15
	}, time.Second*30, time.Second)
}
