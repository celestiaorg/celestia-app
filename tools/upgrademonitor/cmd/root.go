package cmd

import (
	"fmt"
	"os"

	"github.com/celestiaorg/celestia-app/tools/upgrademonitor/internal"
	"github.com/spf13/cobra"
)

// defaultVersion is the value used if the --version flag isn't provided. Since
// v2 is coordinated via an upgrade-height, v3 is the first version that this
// tool supports.
const defaultVersion = uint64(3)

var version uint64

var rootCmd = &cobra.Command{
	Use:   "upgrademonitor grpc-endpoint",
	Short: "upgrademonitor monitors that status of upgrades on a Celestia network.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("must provide a grpc-endpoint")
		}

		grpcEndpoint := args[0]

		err := internal.QueryVersionTally(grpcEndpoint, version)
		if err != nil {
			return err
		}

		return nil
	},
}

func Execute() {
	// Bind the version variable to the --version flag
	rootCmd.Flags().Uint64Var(&version, "version", defaultVersion, "version to monitor")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
