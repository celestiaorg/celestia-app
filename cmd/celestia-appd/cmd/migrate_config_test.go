package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cometbft/cometbft/config"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v6/app"
)

func TestMigrateConfig(t *testing.T) {
	tests := []struct {
		name          string
		version       string
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid v6 migration",
			version:     "v6",
			expectError: false,
		},
		{
			name:          "unsupported version",
			version:       "v99",
			expectError:   true,
			errorContains: "unsupported target version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir := t.TempDir()
			configDir := filepath.Join(tempDir, "config")
			require.NoError(t, os.MkdirAll(configDir, 0755))

			// Create test config files
			setupTestConfigFiles(t, configDir)

			// Run migration
			err := migrateConfig(tempDir, tt.version)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)

				// Verify configs were updated
				verifyMigratedConfigs(t, configDir, tt.version)
			}
		})
	}
}

func TestLoadAndWriteConfigs(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cometConfigPath := filepath.Join(configDir, "config.toml")
	appConfigPath := filepath.Join(configDir, "app.toml")

	// Create test configs
	setupTestConfigFiles(t, configDir)

	t.Run("load and write CometBFT config", func(t *testing.T) {
		cfg, err := loadCometBFTConfig(cometConfigPath, tempDir)
		require.NoError(t, err)
		assert.NotNil(t, cfg)

		originalTimeout := cfg.RPC.TimeoutBroadcastTxCommit
		cfg.RPC.TimeoutBroadcastTxCommit = 60000000000

		config.WriteConfigFile(cometConfigPath, cfg)

		// Load again and verify change persisted
		reloadedCfg, err := loadCometBFTConfig(cometConfigPath, tempDir)
		require.NoError(t, err)
		assert.NotEqual(t, originalTimeout, reloadedCfg.RPC.TimeoutBroadcastTxCommit)
		assert.Equal(t, cfg.RPC.TimeoutBroadcastTxCommit, reloadedCfg.RPC.TimeoutBroadcastTxCommit)
	})

	t.Run("load and write server config", func(t *testing.T) {
		cfg, err := loadServerConfig(appConfigPath)
		require.NoError(t, err)
		assert.NotNil(t, cfg)

		cfg.API.Enable = !cfg.API.Enable
		originalAPIEnable := cfg.API.Enable

		serverconfig.WriteConfigFile(appConfigPath, cfg)

		// Load again and verify change persisted
		reloadedCfg, err := loadServerConfig(appConfigPath)
		require.NoError(t, err)
		assert.Equal(t, originalAPIEnable, reloadedCfg.API.Enable)
	})
}

// setupTestConfigFiles creates test config files with default configurations
func setupTestConfigFiles(t *testing.T, configDir string) {
	t.Helper()

	cometConfigPath := filepath.Join(configDir, "config.toml")
	appConfigPath := filepath.Join(configDir, "app.toml")

	// Create CometBFT config
	cometConfig := app.DefaultConsensusConfig()
	config.WriteConfigFile(cometConfigPath, cometConfig)

	// Create server config
	serverConfig := app.DefaultAppConfig()
	serverconfig.WriteConfigFile(appConfigPath, serverConfig)
}

// verifyMigratedConfigs verifies that configs were properly migrated for the given version
func verifyMigratedConfigs(t *testing.T, configDir, version string) {
	t.Helper()

	cometConfigPath := filepath.Join(configDir, "config.toml")
	appConfigPath := filepath.Join(configDir, "app.toml")

	switch version {
	case "v6":
		// Load and verify CometBFT config has v6 changes
		cometConfig, err := loadCometBFTConfig(cometConfigPath, filepath.Dir(configDir))
		require.NoError(t, err)

		expectedCometConfig := app.DefaultConsensusConfig()
		assert.Equal(t, expectedCometConfig.Mempool.Type, cometConfig.Mempool.Type)

		// Load and verify server config has v6 changes
		serverConfig, err := loadServerConfig(appConfigPath)
		require.NoError(t, err)

		expectedServerConfig := app.DefaultAppConfig()
		assert.Equal(t, expectedServerConfig.MinGasPrices, serverConfig.MinGasPrices)
	}
}
