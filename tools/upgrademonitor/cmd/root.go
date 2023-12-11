package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/tools/upgrademonitor/internal"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

var (
	// defaultVersion is the value used if the version flag isn't provided. Since
	// v2 is coordinated via an upgrade-height, v3 is the first version that this
	// tool supports.
	defaultVersion = uint64(3)
	version        uint64

	// defaultGrpcEndpoint is the value used if the grpc-endpoint flag isn't provided.
	defaultGrpcEndpoint = "0.0.0.0:9090"
	grpcEndpoint        string

	// defaultPollFrequency is the value used if the poll-frequency flag isn't provided.
	// TODO (@rootulp) make this 10 after done developing
	defaultPollFrequency = int64(1)
	pollFrequency        int64

	// defaultAutoTry is the value used if the auto-try flag isn't provided.
	// TODO (@rootulp) make this false after done developing
	defaultAutoTry = true
	autoTry        bool
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
