package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ConfigUpdater defines a function type for applying version-specific updates
type ConfigUpdater func(*config.Config, *serverconfig.Config) (*config.Config, *serverconfig.Config)

// updateRegistry maps version strings to their corresponding update functions
var updateRegistry = map[string]ConfigUpdater{
	"6": applyV6Config,
}

// updateConfigCmd returns the update-config command that updates
// configuration files based on the target version.
func updateConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "update-config",
		Short:   "Update configuration values to be that of a specific app version",
		Long:    "Update configuration files (config.toml and app.toml) to be compatible with a specific app version.",
		Example: "celestia-appd update-config --home ~/.celestia-app\ncelestia-appd update-config --app-version 6 --home ~/.celestia-app --backup false",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := cmd.Flags().GetString(flags.FlagHome)
			if err != nil {
				return err
			}

			backup, err := cmd.Flags().GetBool("backup")
			if err != nil {
				return err
			}

			targetVersion, err := cmd.Flags().GetString("app-version")
			if err != nil {
				return err
			}

			return updateConfig(homeDir, targetVersion, backup)
		},
	}

	cmd.Flags().String(flags.FlagHome, app.NodeHome, "The application home directory")
	cmd.Flags().Bool("backup", true, "Create backups of config files before updating them")
	cmd.Flags().String("app-version", fmt.Sprintf("%d", appconsts.Version), "Target version for config changes")
	return cmd
}

// updateConfig performs the actual update of configuration files.
func updateConfig(homeDir, targetVersion string, backup bool) error {
	fmt.Printf("updating configuration files to version %s...\n", targetVersion)

	updater, exists := updateRegistry[targetVersion]
	if !exists {
		return fmt.Errorf("unsupported target version: %s. Supported versions: %v", targetVersion, getSupportedVersions())
	}

	configDir := filepath.Join(homeDir, "config")
	cometConfigPath := filepath.Join(configDir, "config.toml")
	appConfigPath := filepath.Join(configDir, "app.toml")

	if backup {
		if err := backupConfigFiles(cometConfigPath, appConfigPath); err != nil {
			return fmt.Errorf("failed to backup config files: %w", err)
		}
	}

	cometConfig, err := loadCometBFTConfig(cometConfigPath, homeDir)
	if err != nil {
		return fmt.Errorf("failed to load CometBFT config: %w", err)
	}

	serverConfig, err := loadServerConfig(appConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load server config: %w", err)
	}

	fmt.Printf("Loaded configs successfully. Applying %s config updates...\n", targetVersion)

	cometConfig, serverConfig = updater(cometConfig, serverConfig)

	config.WriteConfigFile(cometConfigPath, cometConfig)
	serverconfig.WriteConfigFile(appConfigPath, serverConfig)

	fmt.Printf("Successfully updated configuration to version %s values\n", targetVersion)
	return nil
}

// loadCometBFTConfig loads the CometBFT configuration from config.toml
func loadCometBFTConfig(configPath, homeDir string) (*config.Config, error) {
	cfg := app.DefaultConsensusConfig()
	cfg.SetRoot(homeDir)

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("toml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}

// loadServerConfig loads the Cosmos SDK server configuration from app.toml
func loadServerConfig(configPath string) (*serverconfig.Config, error) {
	cfg := app.DefaultAppConfig()

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("toml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}

// backupConfigFiles creates timestamped backups of the configuration files
func backupConfigFiles(cometConfigPath, appConfigPath string) error {
	timestamp := time.Now().Format("20060102-150405")

	if err := backupFile(cometConfigPath, timestamp); err != nil {
		return fmt.Errorf("failed to backup config.toml: %w", err)
	}

	if err := backupFile(appConfigPath, timestamp); err != nil {
		return fmt.Errorf("failed to backup app.toml: %w", err)
	}

	fmt.Printf("Created backups with timestamp: %s\n", timestamp)
	return nil
}

// backupFile creates a timestamped backup of a single file
func backupFile(filePath, timestamp string) error {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("Config file %s does not exist, skipping backup\n", filePath)
		return nil
	}

	dir := filepath.Dir(filePath)
	filename := filepath.Base(filePath)
	ext := filepath.Ext(filename)
	name := filename[:len(filename)-len(ext)]

	backupPath := filepath.Join(dir, fmt.Sprintf("%s.%s.backup.%s", name, timestamp, ext))

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write backup file %s: %w", backupPath, err)
	}

	fmt.Printf("Backed up %s to %s\n", filePath, backupPath)
	return nil
}

// getSupportedVersions returns a list of supported updatable versions
func getSupportedVersions() []string {
	versions := make([]string, 0, len(updateRegistry))
	for version := range updateRegistry {
		versions = append(versions, version)
	}
	return versions
}

// applyV6Config applies configuration changes needed for v6
func applyV6Config(cmtCfg *config.Config, appCfg *serverconfig.Config) (*config.Config, *serverconfig.Config) {
	fmt.Println("Applying v6 updates to configs...")

	defaultCfg := app.DefaultConsensusConfig()
	cmtCfg.RPC.MaxBodyBytes = defaultCfg.RPC.MaxBodyBytes

	cmtCfg.Mempool.TTLNumBlocks = defaultCfg.Mempool.TTLNumBlocks
	cmtCfg.Mempool.TTLDuration = defaultCfg.Mempool.TTLDuration
	cmtCfg.Mempool.MaxTxBytes = defaultCfg.Mempool.MaxTxBytes
	cmtCfg.Mempool.MaxTxsBytes = defaultCfg.Mempool.MaxTxsBytes
	cmtCfg.Mempool.Type = defaultCfg.Mempool.Type
	cmtCfg.Mempool.MaxGossipDelay = defaultCfg.Mempool.MaxGossipDelay

	// Only override P2P rates if they're below the minimum
	if cmtCfg.P2P.SendRate < defaultCfg.P2P.SendRate {
		cmtCfg.P2P.SendRate = defaultCfg.P2P.SendRate
	}
	if cmtCfg.P2P.RecvRate < defaultCfg.P2P.RecvRate {
		cmtCfg.P2P.RecvRate = defaultCfg.P2P.RecvRate
	}

	defaultAppCfg := app.DefaultAppConfig()

	// only unset the min gas price if it's the legacy default (i.e. untouched)
	if appCfg.MinGasPrices == fmt.Sprintf("%v%s", appconsts.LegacyDefaultMinGasPrice, appconsts.BondDenom) {
		appCfg.MinGasPrices = ""
	}
	appCfg.GRPC.MaxRecvMsgSize = defaultAppCfg.GRPC.MaxRecvMsgSize

	return cmtCfg, appCfg
}
