package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
)

// ConfigMigrator defines a function type for applying version-specific migrations
type ConfigMigrator func(*config.Config, *serverconfig.Config) (*config.Config, *serverconfig.Config)

// migrationRegistry maps version strings to their corresponding migration functions
var migrationRegistry = map[string]ConfigMigrator{
	"v6": applyV6Migrations,
}

// migrateConfigCmd returns the migrate-config command that migrates
// configuration files based on the target version.
func migrateConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "migrate-config",
		Short:   "Migrate configuration files to a target version",
		Long:    "Migrate configuration files (config.toml and app.toml) to be compatible with a target application version.",
		Example: "celestia-appd migrate-config --home ~/.celestia-app\ncelestia-appd migrate-config --version v5 --home ~/.celestia-app",
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

			targetVersion, err := cmd.Flags().GetString("version")
			if err != nil {
				return err
			}
			return migrateConfig(homeDir, targetVersion, backup)
		},
	}

	cmd.Flags().String(flags.FlagHome, app.NodeHome, "The application home directory")
	cmd.Flags().Bool("backup", false, "Create backups of config files before migrating")
	cmd.Flags().String("version", fmt.Sprintf("v%d", appconsts.Version), "Target version for migration")
	return cmd
}

// migrateConfig performs the actual migration of configuration files.
func migrateConfig(homeDir, targetVersion string, backup bool) error {
	fmt.Printf("Migrating configuration files to version %s...\n", targetVersion)

	migrator, exists := migrationRegistry[targetVersion]
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

	fmt.Printf("Loaded configs successfully. Applying %s migrations...\n", targetVersion)

	cometConfig, serverConfig = migrator(cometConfig, serverConfig)

	config.WriteConfigFile(cometConfigPath, cometConfig)
	serverconfig.WriteConfigFile(appConfigPath, serverConfig)

	fmt.Printf("Successfully migrated configuration files to version %s\n", targetVersion)
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

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup file %s: %w", backupPath, err)
	}

	fmt.Printf("Backed up %s to %s\n", filePath, backupPath)
	return nil
}

// getSupportedVersions returns a list of supported migration versions
func getSupportedVersions() []string {
	versions := make([]string, 0, len(migrationRegistry))
	for version := range migrationRegistry {
		versions = append(versions, version)
	}
	return versions
}

// applyV6Migrations applies configuration changes needed for v6
func applyV6Migrations(cmtCfg *config.Config, appCfg *serverconfig.Config) (*config.Config, *serverconfig.Config) {
	fmt.Println("Applying v6 configuration migrations...")

	defaultCfg := app.DefaultConsensusConfig()
	cmtCfg.RPC.MaxBodyBytes = defaultCfg.RPC.MaxBodyBytes

	cmtCfg.Mempool.TTLNumBlocks = defaultCfg.Mempool.TTLNumBlocks
	cmtCfg.Mempool.TTLDuration = defaultCfg.Mempool.TTLDuration
	cmtCfg.Mempool.MaxTxBytes = defaultCfg.Mempool.MaxTxBytes
	cmtCfg.Mempool.MaxTxsBytes = defaultCfg.Mempool.MaxTxsBytes
	cmtCfg.Mempool.Type = defaultCfg.Mempool.Type
	cmtCfg.Mempool.MaxGossipDelay = defaultCfg.Mempool.MaxGossipDelay

	cmtCfg.P2P.SendRate = defaultCfg.P2P.SendRate
	cmtCfg.P2P.RecvRate = defaultCfg.P2P.RecvRate

	defaultAppCfg := app.DefaultAppConfig()
	appCfg.MinGasPrices = defaultAppCfg.MinGasPrices
	appCfg.GRPC.MaxRecvMsgSize = defaultAppCfg.GRPC.MaxRecvMsgSize

	return cmtCfg, appCfg
}
