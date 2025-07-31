package cmd

import (
	"fmt"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/multiplexer/appd"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

type PreStartHook func(cmd *cobra.Command, logger log.Logger) error

// addPreStartHooks finds the start command and adds pre-start hooks using Cobra's PreRunE
func addPreStartHooks(rootCmd *cobra.Command, hooks ...PreStartHook) error {
	// find start command
	startCmd, _, err := rootCmd.Find([]string{"start"})
	if err != nil {
		return fmt.Errorf("failed to find start command: %w", err)
	}

	// Add the pre-start hooks
	startCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		sctx := server.GetServerContextFromCmd(cmd)
		logger := sctx.Logger

		for _, hook := range hooks {
			if err := hook(cmd, logger); err != nil {
				return err
			}
		}
		return nil
	}

	return nil
}

// setMultiplexerHome sets the home directory for the multiplexer based on the --home flag
func setMultiplexerHome(cmd *cobra.Command, logger log.Logger) error {
	clientCtx := client.GetClientContextFromCmd(cmd)
	if clientCtx.HomeDir != "" {
		appd.SetNodeHome(clientCtx.HomeDir)
		logger.Info("Set multiplexer home directory", "home", clientCtx.HomeDir)
	}
	return nil
}
