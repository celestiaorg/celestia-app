package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/app"
	tmcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestOverrideP2PConfig_Integration(t *testing.T) {
	testCases := []struct {
		name                string
		setupConfig         func(*tmcfg.Config)
		expectedSendRate    int64
		expectedRecvRate    int64
		expectedTTLBlocks   int64
		expectedTTLDur      time.Duration
		expectedGossipDelay time.Duration
		expectedMaxTxsBytes int64
		expectedMempoolType string
	}{
		{
			name: "Override P2P rates below minimum",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.P2P.SendRate = 10 * mebibyte
				cfg.P2P.RecvRate = 10 * mebibyte
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Preserve P2P rates above minimum",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.P2P.SendRate = 50 * mebibyte
				cfg.P2P.RecvRate = 50 * mebibyte
			},
			expectedSendRate:    50 * mebibyte,
			expectedRecvRate:    50 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Override TTLNumBlocks when less than 36",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.Mempool.TTLNumBlocks = 12
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Preserve TTLNumBlocks when 0 (disabled)",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.Mempool.TTLNumBlocks = 0
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   0,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Preserve TTLNumBlocks when greater than 36",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.Mempool.TTLNumBlocks = 100
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   100,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Force TTLDuration to 0",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.Mempool.TTLDuration = 10 * time.Minute
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Override MaxGossipDelay from 60s to 20s",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.Mempool.MaxGossipDelay = 60 * time.Second
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Preserve custom MaxGossipDelay (not 60s)",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.Mempool.MaxGossipDelay = 30 * time.Second
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 30 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Override MaxTxsBytes when less than 400 MiB",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.Mempool.MaxTxsBytes = 200 * mebibyte
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Preserve MaxTxsBytes when greater than 400 MiB",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.Mempool.MaxTxsBytes = 500 * mebibyte
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 500 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Override all configs that need it",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.P2P.SendRate = 5 * mebibyte
				cfg.P2P.RecvRate = 5 * mebibyte
				cfg.Mempool.TTLNumBlocks = 10
				cfg.Mempool.TTLDuration = 5 * time.Minute
				cfg.Mempool.MaxGossipDelay = 60 * time.Second
				cfg.Mempool.MaxTxsBytes = 100 * mebibyte
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Override mempool type from flood to CAT",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.Mempool.Type = tmcfg.MempoolTypeFlood
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
		{
			name: "Preserve mempool type when already CAT",
			setupConfig: func(cfg *tmcfg.Config) {
				cfg.Mempool.Type = tmcfg.MempoolTypeCAT
			},
			expectedSendRate:    24 * mebibyte,
			expectedRecvRate:    24 * mebibyte,
			expectedTTLBlocks:   36,
			expectedTTLDur:      0,
			expectedGossipDelay: 20 * time.Second,
			expectedMaxTxsBytes: 400 * mebibyte,
			expectedMempoolType: tmcfg.MempoolTypeCAT,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tempDir := t.TempDir()
			configDir := filepath.Join(tempDir, "config")
			require.NoError(t, os.MkdirAll(configDir, 0o755))

			// Create a config with test-specific values
			cfg := app.DefaultConsensusConfig()
			cfg.SetRoot(tempDir)
			tc.setupConfig(cfg)

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
			err = overrideP2PConfig(cmd, logger)
			require.NoError(t, err)

			// Get the modified config from the server context
			modifiedCfg := server.GetServerContextFromCmd(cmd).Config

			// Assert P2P values
			require.Equal(t, tc.expectedSendRate, modifiedCfg.P2P.SendRate,
				"P2P SendRate should be %d", tc.expectedSendRate)
			require.Equal(t, tc.expectedRecvRate, modifiedCfg.P2P.RecvRate,
				"P2P RecvRate should be %d", tc.expectedRecvRate)

			// Assert mempool values
			require.Equal(t, tc.expectedMempoolType, modifiedCfg.Mempool.Type,
				"Mempool Type should be %s", tc.expectedMempoolType)
			require.Equal(t, tc.expectedTTLBlocks, modifiedCfg.Mempool.TTLNumBlocks,
				"Mempool TTLNumBlocks should be %d", tc.expectedTTLBlocks)
			require.Equal(t, tc.expectedTTLDur, modifiedCfg.Mempool.TTLDuration,
				"Mempool TTLDuration should be %v", tc.expectedTTLDur)
			require.Equal(t, tc.expectedGossipDelay, modifiedCfg.Mempool.MaxGossipDelay,
				"Mempool MaxGossipDelay should be %v", tc.expectedGossipDelay)
			require.Equal(t, tc.expectedMaxTxsBytes, modifiedCfg.Mempool.MaxTxsBytes,
				"Mempool MaxTxsBytes should be %d", tc.expectedMaxTxsBytes)
		})
	}
}

// TestOverrideP2PConfig_ConfigPersistence tests that overrides happen in memory
// and don't write back to the config file
func TestOverrideP2PConfig_ConfigPersistence(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	// Create a config with values that should be overridden
	cfg := app.DefaultConsensusConfig()
	cfg.SetRoot(tempDir)
	cfg.P2P.SendRate = 5 * mebibyte
	cfg.P2P.RecvRate = 5 * mebibyte
	cfg.Mempool.TTLNumBlocks = 10
	cfg.Mempool.TTLDuration = 5 * time.Minute
	cfg.Mempool.MaxGossipDelay = 60 * time.Second

	// Write the config to disk
	configPath := filepath.Join(configDir, "config.toml")
	tmcfg.WriteConfigFile(configPath, cfg)

	// Create a mock cobra command with server context
	cmd := &cobra.Command{
		Use: "test",
	}
	logger := log.NewNopLogger()

	// Load the config from disk
	loadedCfg, err := loadCometBFTConfig(configPath, tempDir)
	require.NoError(t, err)

	// Create and set server context
	sctx := server.NewDefaultContext()
	sctx.Config = loadedCfg
	sctx.Logger = logger

	// Set the context on the command
	ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
	cmd.SetContext(ctx)

	// Run the override function
	err = overrideP2PConfig(cmd, logger)
	require.NoError(t, err)

	// Verify in-memory config was modified
	modifiedCfg := server.GetServerContextFromCmd(cmd).Config
	require.Equal(t, int64(24*mebibyte), modifiedCfg.P2P.SendRate)
	require.Equal(t, int64(36), modifiedCfg.Mempool.TTLNumBlocks)

	// Read the config file again to verify it wasn't changed on disk
	fileConfig, err := loadCometBFTConfig(configPath, tempDir)
	require.NoError(t, err)

	// The file should still have the original values
	require.Equal(t, int64(5*mebibyte), fileConfig.P2P.SendRate,
		"Config file should not be modified by override")
	require.Equal(t, int64(10), fileConfig.Mempool.TTLNumBlocks,
		"Config file should not be modified by override")
}

// TestOverrideP2PConfig_BypassFlag tests that the bypass flag prevents all overrides
func TestOverrideP2PConfig_BypassFlag(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	// Create a config with values that would normally be overridden
	cfg := app.DefaultConsensusConfig()
	cfg.SetRoot(tempDir)
	cfg.P2P.SendRate = 5 * mebibyte
	cfg.P2P.RecvRate = 5 * mebibyte
	cfg.Mempool.Type = tmcfg.MempoolTypeFlood
	cfg.Mempool.TTLNumBlocks = 10
	cfg.Mempool.TTLDuration = 5 * time.Minute
	cfg.Mempool.MaxGossipDelay = 60 * time.Second
	cfg.Mempool.MaxTxsBytes = 100 * mebibyte

	// Write the config to disk
	configPath := filepath.Join(configDir, "config.toml")
	tmcfg.WriteConfigFile(configPath, cfg)

	// Create a mock cobra command with server context
	cmd := &cobra.Command{
		Use: "test",
	}
	// Add the bypass flag to the command
	cmd.Flags().Bool(bypassOverridesFlagKey, false, "bypass all config overrides")
	// Set the bypass flag to true
	require.NoError(t, cmd.Flags().Set(bypassOverridesFlagKey, "true"))

	logger := log.NewNopLogger()

	// Load the config from disk
	loadedCfg, err := loadCometBFTConfig(configPath, tempDir)
	require.NoError(t, err)

	// Create and set server context
	sctx := server.NewDefaultContext()
	sctx.Config = loadedCfg
	sctx.Logger = logger

	// Set the context on the command
	ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
	cmd.SetContext(ctx)

	// Run the override function
	err = overrideP2PConfig(cmd, logger)
	require.NoError(t, err)

	// Verify that the config was NOT modified because of the bypass flag
	modifiedCfg := server.GetServerContextFromCmd(cmd).Config
	require.Equal(t, int64(5*mebibyte), modifiedCfg.P2P.SendRate,
		"P2P SendRate should not be overridden when bypass flag is set")
	require.Equal(t, int64(5*mebibyte), modifiedCfg.P2P.RecvRate,
		"P2P RecvRate should not be overridden when bypass flag is set")
	require.Equal(t, tmcfg.MempoolTypeFlood, modifiedCfg.Mempool.Type,
		"Mempool Type should not be overridden when bypass flag is set")
	require.Equal(t, int64(10), modifiedCfg.Mempool.TTLNumBlocks,
		"Mempool TTLNumBlocks should not be overridden when bypass flag is set")
	require.Equal(t, 5*time.Minute, modifiedCfg.Mempool.TTLDuration,
		"Mempool TTLDuration should not be overridden when bypass flag is set")
	require.Equal(t, 60*time.Second, modifiedCfg.Mempool.MaxGossipDelay,
		"Mempool MaxGossipDelay should not be overridden when bypass flag is set")
	require.Equal(t, int64(100*mebibyte), modifiedCfg.Mempool.MaxTxsBytes,
		"Mempool MaxTxsBytes should not be overridden when bypass flag is set")
}
