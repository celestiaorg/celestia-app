package cmd

import (
	"fmt"
	"os"

	"github.com/celestiaorg/celestia-app/tools/upgrademonitor/internal"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "upgrademonitor grpc-endpoint",
	Short: "upgrademonitor monitors that status of upgrades on a Celestia network.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("must provide a grpc-endpoint")
		}

		grpcEndpoint := args[0]
		err := internal.QueryVersionTally(grpcEndpoint)
		if err != nil {
			return err
		}

		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
