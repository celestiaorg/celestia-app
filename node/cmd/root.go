package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/node/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

const rootDir = "rootDir"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use: "node",
	RunE: func(cmd *cobra.Command, args []string) error {
		multiplexer := utils.NewMultiplexer()

		config := utils.GetConfig()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := os.Mkdir(rootDir, 0755)
		if err != nil {
			return err
		}
		defer os.RemoveAll(rootDir)
		cctx, cleanup, err := utils.StartNode(ctx, config, multiplexer, rootDir)
		defer cleanup()
		if err != nil {
			fmt.Printf("failed to start node: %v\n", err)
			return err
		}
		err = cctx.WaitForNextBlock()
		if err != nil {
			fmt.Printf("waiting for next block failed: %v\n", err)
		}
		time.Sleep(10 * time.Second)
		return nil
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
