package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/cometbft/cometbft/config"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateConfig(t *testing.T) {
	tests := []struct {
		name          string
		version       string
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid v6 update",
			version:     "6",
			expectError: false,
		},
		{
			name:          "unsupported version",
			version:       "99",
			expectError:   true,
			errorContains: "unsupported target version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir := t.TempDir()
			configDir := filepath.Join(tempDir, "config")
			require.NoError(t, os.MkdirAll(configDir, 0o755))

			// Create test config files
			setupTestConfigFiles(t, configDir)

			// Run update
			err := updateConfig(tempDir, tt.version, false)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)

				// Verify configs were updated
				verifyUpdatedConfigs(t, configDir, tt.version)
			}
		})
	}
}

func TestLoadAndWriteConfigs(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

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

		newConfigVal := !cfg.API.Enable
		cfg.API.Enable = newConfigVal

		serverconfig.WriteConfigFile(appConfigPath, cfg)

		// Load again and verify change persisted
		reloadedCfg, err := loadServerConfig(appConfigPath)
		require.NoError(t, err)
		assert.Equal(t, newConfigVal, reloadedCfg.API.Enable)
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

// verifyUpdatedConfigs verifies that configs were properly updated for the given version
func verifyUpdatedConfigs(t *testing.T, configDir, version string) {
	t.Helper()

	cometConfigPath := filepath.Join(configDir, "config.toml")
	appConfigPath := filepath.Join(configDir, "app.toml")

	if version == "v6" {
		cometConfig, err := loadCometBFTConfig(cometConfigPath, filepath.Dir(configDir))
		require.NoError(t, err)

		expectedCometConfig := app.DefaultConsensusConfig()
		assert.Equal(t, expectedCometConfig.Mempool.Type, cometConfig.Mempool.Type)

		serverConfig, err := loadServerConfig(appConfigPath)
		require.NoError(t, err)

		expectedServerConfig := app.DefaultAppConfig()
		assert.Equal(t, expectedServerConfig.MinGasPrices, serverConfig.MinGasPrices)
	}
}

func TestUpdateMinGasPrices(t *testing.T) {
	t.Run("legacy default min gas price is unset to empty string", func(t *testing.T) {
		// Setup configs with legacy default min gas price
		cmtCfg := app.DefaultConsensusConfig()
		appCfg := app.DefaultAppConfig()
		// Set legacy default min gas price explicitly
		appCfg.MinGasPrices = fmt.Sprintf("%v%s", appconsts.LegacyDefaultMinGasPrice, appconsts.BondDenom)

		_, updatedAppCfg := applyV6Config(cmtCfg, appCfg)

		assert.Equal(t, "", updatedAppCfg.MinGasPrices, "MinGasPrices should be unset to empty string if legacy default")
	})

	t.Run("custom min gas price is preserved", func(t *testing.T) {
		// Setup configs with a custom min gas price
		cmtCfg := app.DefaultConsensusConfig()
		appCfg := app.DefaultAppConfig()
		customGasPrice := "0.123utia"
		appCfg.MinGasPrices = customGasPrice

		_, updatedAppCfg := applyV6Config(cmtCfg, appCfg)

		assert.Equal(t, customGasPrice, updatedAppCfg.MinGasPrices, "MinGasPrices should remain unchanged if not legacy default")
	})
}
