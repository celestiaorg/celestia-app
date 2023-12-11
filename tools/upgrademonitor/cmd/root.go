package cmd

import (
	"fmt"
	"os"
	"time"

	// import celestia-app for it's side-effects so that celestia-app init()
	// overrides the Cosmos SDK config with the correct account address
	// prefixes.
	_ "github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/tools/upgrademonitor/internal"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "upgrademonitor",
	Short: "upgrademonitor monitors that status of upgrades on a Celestia network.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateSigner(autoTry, signer); err != nil {
			return err
		}

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
					addr, err := sdk.AccAddressFromBech32(signer)
					if err != nil {
						return err
					}
					internal.SubmitTryUpgrade(grpcEndpoint, addr)
				}
			}
		}
	},
}

func Execute() {
	// Bind the version variable to the --version flag
	rootCmd.Flags().Uint64Var(&version, "version", defaultVersion, "version to monitor")
	// Bind the grpcEndpoint variable to the --grpc-endpoint flag
	rootCmd.Flags().StringVar(&grpcEndpoint, "grpc-endpoint", defaultGrpcEndpoint, "GRPC endpoint of a consensus node")
	// Bind the pollFrequency variable to the --poll-frequency flag
	rootCmd.Flags().Int64Var(&pollFrequency, "poll-frequency", defaultPollFrequency, "poll frequency in seconds")
	// Bind the autoTry variable to the --auto-try flag
	rootCmd.Flags().BoolVar(&autoTry, "auto-try", defaultAutoTry, "auto try upgrade if the network is upgradeable")
	// Bind the signer variable to the --signer flag
	rootCmd.Flags().StringVar(&signer, "signer", defaultSigner, "signer is the Celestia address that should be used to submit the try upgrade")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func validateSigner(autoTry bool, signer string) error {
	if !autoTry {
		return nil
	}
	if signer == "" {
		return fmt.Errorf("invalid signer. Must specify signer if autoTry is enabled.")
	}
	if _, err := sdk.AccAddressFromBech32(signer); err != nil {
		return err
	}
	return nil
}
