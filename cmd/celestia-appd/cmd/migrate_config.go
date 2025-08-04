package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/celestiaorg/celestia-app/v6/app"
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
		Use:     "migrate-config [version]",
		Short:   "Migrate configuration files to a target version",
		Long:    "Migrate configuration files (config.toml and app.toml) to be compatible with a target application version.",
		Example: "celestia-appd migrate-config v6 --home ~/.celestia-app",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := cmd.Flags().GetString(flags.FlagHome)
			if err != nil {
				return err
			}

			targetVersion := args[0]
			return migrateConfig(homeDir, targetVersion)
		},
	}

	cmd.Flags().String(flags.FlagHome, app.NodeHome, "The application home directory")
	return cmd
}

// migrateConfig performs the actual migration of configuration files.
func migrateConfig(homeDir, targetVersion string) error {
	fmt.Printf("Migrating configuration files to version %s...\n", targetVersion)

	migrator, exists := migrationRegistry[targetVersion]
	if !exists {
		return fmt.Errorf("unsupported target version: %s. Supported versions: %v", targetVersion, getSupportedVersions())
	}

	configDir := filepath.Join(homeDir, "config")
	cometConfigPath := filepath.Join(configDir, "config.toml")
	appConfigPath := filepath.Join(configDir, "app.toml")

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
