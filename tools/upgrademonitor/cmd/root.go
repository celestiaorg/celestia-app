package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/tools/upgrademonitor/internal"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "upgrademonitor",
	Short: "upgrademonitor monitors that status of upgrades on a Celestia network.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ticker := time.NewTicker(time.Duration(pollFrequency) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				resp, err := internal.QueryVersionTally(grpcEndpoint, version)
				if err != nil {
					return err
				}
				fmt.Printf("version: %v, voting: %v, threshold: %v, total: %v\n", version, resp.GetVotingPower(), resp.GetThresholdPower(), resp.GetTotalVotingPower())

				if autoTry && internal.IsUpgradeable(resp) {
					// TODO (@rootulp): get signer
					signer := sdk.AccAddress{}
					internal.SubmitTryUpgrade(grpcEndpoint, signer)
				}
			}
		}
	},
}

func Execute() {
	// Bind the version variable to the --version flag
	rootCmd.Flags().Uint64Var(&version, "version", defaultVersion, "version to monitor")

	// Bind the grpcEndpoint variable to the --grpc-endpoint flag
	rootCmd.Flags().StringVar(&grpcEndpoint, "grpc-endpoint", defaultGrpcEndpoint, "GRPC endpoint")

	// Bind the pollFrequency variable to the --poll-frequency flag
	rootCmd.Flags().Int64Var(&pollFrequency, "poll-frequency", defaultPollFrequency, "poll frequency in seconds")

	// Bind the autoTry variable to the --auto-try flag
	rootCmd.Flags().BoolVar(&autoTry, "auto-try", defaultAutoTry, "auto try upgrade")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
