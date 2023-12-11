package cmd

import (
	"fmt"
	"os"

	"github.com/celestiaorg/celestia-app/tools/upgrademonitor/internal"
	"github.com/spf13/cobra"
)

var (
	version uint64
	// defaultVersion is the value used if the --version flag isn't provided. Since
	// v2 is coordinated via an upgrade-height, v3 is the first version that this
	// tool supports.
	defaultVersion = uint64(3)
)

var (
	grpcEndpoint string
	// defaultGrpcEndpoint is the value used if the --grpc-endpoint flag isn't provided.
	defaultGrpcEndpoint = "0.0.0.0:9090"
)

var rootCmd = &cobra.Command{
	Use:   "upgrademonitor",
	Short: "upgrademonitor monitors that status of upgrades on a Celestia network.",
	RunE: func(cmd *cobra.Command, args []string) error {
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

	// Bind the grpcEndpoint variable to the --grpc-endpoint flag
	rootCmd.Flags().StringVar(&grpcEndpoint, "grpc-endpoint", defaultGrpcEndpoint, "GRPC endpoint")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
