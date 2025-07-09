package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/celestiaorg/celestia-app/v5/app"
	"github.com/celestiaorg/celestia-app/v5/app/params"
	"github.com/celestiaorg/celestia-app/v5/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	// Old default minimum gas price that needs to be updated
	oldDefaultMinGasPrice = "0.002utia"
)

// checkAndUpdateMinGasPrices checks if the minimum gas prices in app.toml
// are set to the old default value and updates them if necessary.
func checkAndUpdateMinGasPrices(cmd *cobra.Command) error {
	// Get the home directory from the command
	homeDir, err := cmd.Flags().GetString(flags.FlagHome)
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	if homeDir == "" {
		homeDir = app.NodeHome
	}

	// Construct the path to app.toml
	appConfigPath := filepath.Join(homeDir, "config", "app.toml")

	// Check if the file exists
	if _, err := os.Stat(appConfigPath); os.IsNotExist(err) {
		// File doesn't exist, nothing to update
		return nil
	}

	// Read the current app.toml file
	viper.SetConfigFile(appConfigPath)
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read app.toml: %w", err)
	}

	// Get the current minimum gas prices
	currentMinGasPrices := viper.GetString("minimum-gas-prices")

	// Check if it matches the old default
	if strings.TrimSpace(currentMinGasPrices) == oldDefaultMinGasPrice {
		// Update to the new default
		minGasPrice := fmt.Sprintf("%v%s", appconsts.DefaultMinGasPrice, params.BondDenom)
		viper.Set("minimum-gas-prices", minGasPrice)

		// Write the updated configuration back to the file
		if err := viper.WriteConfig(); err != nil {
			return fmt.Errorf("failed to write updated app.toml: %w", err)
		}

		// Log the update
		logMessage := fmt.Sprintf("Updated minimum gas prices in %s: %s -> %s",
			appConfigPath, oldDefaultMinGasPrice, minGasPrice)
		fmt.Println(logMessage)
	}

	return nil
}
