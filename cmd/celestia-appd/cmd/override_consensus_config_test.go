package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	tmcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverrideConsensusConfig_Integration(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	// Create a config with test-specific values
	cfg := app.DefaultConsensusConfig()
	cfg.SetRoot(tempDir)

	// Write the config to disk
	configPath := filepath.Join(configDir, "config.toml")
	tmcfg.WriteConfigFile(configPath, cfg)

	// Create a mock cobra command with server context
	cmd := &cobra.Command{
		Use: "test",
	}
	logger := log.NewNopLogger()

	// Load the config from disk (simulating what happens during startup)
	loadedCfg, err := loadCometBFTConfig(configPath, tempDir)
	require.NoError(t, err)

	// Create and set server context
	sctx := server.NewDefaultContext()
	sctx.Config = loadedCfg
	sctx.Logger = logger

	// Set the context on the command
	ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
	cmd.SetContext(ctx)

	// Run the override function (this is what happens in PreRunE)
	err = overrideConsensusTimeouts(cmd, logger)
	require.NoError(t, err)

	// Get the modified config from the server context
	modifiedCfg := server.GetServerContextFromCmd(cmd).Config

	assert.Equal(t, appconsts.TimeoutPropose, modifiedCfg.Consensus.TimeoutPropose)
	assert.Equal(t, appconsts.TimeoutPrevote, modifiedCfg.Consensus.TimeoutPrevote)
	assert.Equal(t, appconsts.TimeoutPrevoteDelta, modifiedCfg.Consensus.TimeoutPrevoteDelta)
	assert.Equal(t, appconsts.TimeoutPrecommit, modifiedCfg.Consensus.TimeoutPrecommit)
	assert.Equal(t, appconsts.TimeoutPrecommitDelta, modifiedCfg.Consensus.TimeoutPrecommitDelta)
	assert.Equal(t, appconsts.TimeoutCommit, modifiedCfg.Consensus.TimeoutCommit)
}
