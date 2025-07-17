package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v5/app"
	"github.com/celestiaorg/celestia-app/v5/app/params"
	"github.com/celestiaorg/celestia-app/v5/pkg/appconsts"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	oldDefaultMinGasPrice = "0.002utia"
	minGasPriceKey        = "minimum-gas-prices"
)

// checkAndUpdateMinGasPrices checks if the minimum gas prices in app.toml
// are set to the old default value and updates them if necessary.
func checkAndUpdateMinGasPrices(cmd *cobra.Command, logger log.Logger) error {
	appConfigPath := filepath.Join(app.NodeHome, "config", "app.toml")
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
	currentMinGasPrices := viper.GetString(minGasPriceKey)

	// Check if it matches the old default
	if strings.TrimSpace(currentMinGasPrices) == oldDefaultMinGasPrice {
		// Update to the new default
		minGasPrice := fmt.Sprintf("%v%s", appconsts.DefaultMinGasPrice, params.BondDenom)

		// Update the file by reading it line by line and replacing only the specific line
		updated, err := updateConfigFile(appConfigPath, minGasPriceKey, minGasPrice)
		if err != nil {
			return fmt.Errorf("failed to update app.toml: %w", err)
		} else if !updated {
			return fmt.Errorf("failed to update app.toml, key not found: %s", minGasPriceKey)
		}

		logger.Info("Updated minimum gas prices",
			"file", appConfigPath,
			"old_value", oldDefaultMinGasPrice,
			"new_value", minGasPrice)
	} else {
		logger.Debug("Minimum gas prices are already updated")
	}

	return nil
}

// updateConfigFile updates only the minimum-gas-prices line in the config file
// while preserving all other content, comments, and formatting
func updateConfigFile(filePath, key, newValue string) (bool, error) {
	// Read the file
	file, err := os.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)

	updated := false
	for scanner.Scan() {
		line := scanner.Text()
		// Check if this line contains the minimum-gas-prices setting
		if strings.HasPrefix(line, key) {
			lines = append(lines, fmt.Sprintf("%s = \"%s\"", key, newValue))
			updated = true
		} else {
			// Keep the line unchanged
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("error reading file: %w", err)
	}

	// Write the updated content back to the file
	if err := os.WriteFile(filePath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return false, fmt.Errorf("failed to write file: %w", err)
	}

	return updated, nil
}
