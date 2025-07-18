package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckAndUpdateMinGasPrices(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "gas-price-test")
	require.NoError(t, err)
	dir := app.NodeHome
	app.NodeHome = tempDir
	defer func() {
		app.NodeHome = dir
	}()
	defer os.RemoveAll(tempDir)

	// Create config directory
	configDir := filepath.Join(tempDir, "config")
	err = os.MkdirAll(configDir, 0o755)
	require.NoError(t, err)

	// Create app.toml file with old default
	appConfigPath := filepath.Join(configDir, "app.toml")
	appConfigContent := `minimum-gas-prices = "0.002utia"
`
	err = os.WriteFile(appConfigPath, []byte(appConfigContent), 0o644)
	require.NoError(t, err)

	// Create a mock command
	cmd := &cobra.Command{}
	cmd.Flags().String("home", tempDir, "home directory")

	// Test the function
	err = checkAndUpdateMinGasPrices(cmd, log.NewTestLogger(t))
	require.NoError(t, err)

	// Verify the file was updated
	viper.SetConfigFile(appConfigPath)
	err = viper.ReadInConfig()
	require.NoError(t, err)

	updatedMinGasPrices := viper.GetString("minimum-gas-prices")
	assert.Equal(t, "0.004utia", updatedMinGasPrices)
}

func TestCheckAndUpdateMinGasPricesNoUpdate(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "gas-price-test")
	require.NoError(t, err)
	dir := app.NodeHome
	app.NodeHome = tempDir
	defer func() {
		app.NodeHome = dir
	}()
	defer os.RemoveAll(tempDir)

	// Create config directory
	configDir := filepath.Join(tempDir, "config")
	err = os.MkdirAll(configDir, 0o755)
	require.NoError(t, err)

	// Create app.toml file with new default (should not be updated)
	appConfigPath := filepath.Join(configDir, "app.toml")
	appConfigContent := `minimum-gas-prices = "0.004utia"
`
	err = os.WriteFile(appConfigPath, []byte(appConfigContent), 0o644)
	require.NoError(t, err)

	// Create a mock command
	cmd := &cobra.Command{}
	cmd.Flags().String("home", tempDir, "home directory")

	// Test the function
	err = checkAndUpdateMinGasPrices(cmd, log.NewNopLogger())
	require.NoError(t, err)

	// Verify the file was not changed
	viper.SetConfigFile(appConfigPath)
	err = viper.ReadInConfig()
	require.NoError(t, err)

	updatedMinGasPrices := viper.GetString("minimum-gas-prices")
	assert.Equal(t, "0.004utia", updatedMinGasPrices)
}

func TestCheckAndUpdateMinGasPricesFileNotExists(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "gas-price-test")
	require.NoError(t, err)
	dir := app.NodeHome
	app.NodeHome = tempDir
	defer func() {
		app.NodeHome = dir
	}()
	defer os.RemoveAll(tempDir)

	// Create a mock command
	cmd := &cobra.Command{}
	cmd.Flags().String("home", tempDir, "home directory")

	// Test the function - should not error when file doesn't exist
	err = checkAndUpdateMinGasPrices(cmd, log.NewNopLogger())
	require.NoError(t, err)
}
