package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/node/utils"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use: "node",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create a logger
		logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))

		// Load configuration
		cfg := config.DefaultConfig()
		cfg.RootDir = "."
		cfg.P2P.ListenAddress = "tcp://0.0.0.0:26656"
		cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"

		// Initialize a CometBFT node
		apps := utils.GetApps()
		// currentAppVersion := uint64(1)
		// multiplexer := utils.NewMultiplexer(currentAppVersion, apps)
		// utils.NewMultiplexer(currentAppVersion, apps)
		proxyApp := proxy.NewLocalClientCreator(apps[0])

		nodeKey, err := p2p.LoadOrGenNodeKey(filepath.Join(cfg.RootDir, "config", "node_key.json"))
		if err != nil {
			logger.Error("failed to load or generate node key", "error", err)
			return err
		}

		privValidatorKeyFile := filepath.Join(cfg.RootDir, "config", "priv_validator_key.json")
		privValidatorStateFile := filepath.Join(cfg.RootDir, "data", "priv_validator_state.json")
		privValidator := privval.LoadOrGenFilePV(privValidatorKeyFile, privValidatorStateFile)
		config := testnode.DefaultConfig()

		genesisDocProvider := node.DefaultGenesisDocProviderFunc(cfg)

		node, err := node.NewNode(
			cfg,
			privValidator,
			nodeKey,
			proxyApp,
			genesisDocProvider,
			node.DefaultDBProvider,
			node.DefaultMetricsProvider(config.TmConfig.Instrumentation),
			logger,
		)
		if err != nil {
			logger.Error("failed to create node", "error", err)
			return err
		}

		// Start the CometBFT node
		if err := node.Start(); err != nil {
			logger.Error("failed to start node", "error", err)
			return err
		}

		// Wait for the node to shut down
		defer func() {
			if err := node.Stop(); err != nil {
				logger.Error("failed to stop node", "error", err)
			}
		}()

		// Keep the process running
		select {}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.node.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".node" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".node")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
