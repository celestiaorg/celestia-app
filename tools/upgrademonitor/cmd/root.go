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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var rootCmd = &cobra.Command{
	Use:   "upgrademonitor",
	Short: "upgrademonitor monitors that status of upgrades on a Celestia network.",
	RunE: func(cmd *cobra.Command, args []string) error {
		conn, err := grpc.Dial(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return fmt.Errorf("could not connect: %v", err)
		}
		defer conn.Close()

		ticker := time.NewTicker(time.Duration(pollFrequency) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				resp, err := internal.QueryVersionTally(conn, version)
				if err != nil {
					return err
				}
				fmt.Printf("version: %v, voting: %v, threshold: %v, total: %v\n", version, resp.GetVotingPower(), resp.GetThresholdPower(), resp.GetTotalVotingPower())

				if internal.IsUpgradeable(resp) {
					fmt.Printf("the network is upgradeable so publishing %v\n", pathToTransaction)
					resp, err := internal.Publish(conn, pathToTransaction)
					if err != nil {
						return err
					}
					fmt.Printf("published transaction: %v\n", resp.TxHash)
					return nil // stop the upgrademonitor
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
	// Bind the pathToTransaction variable to the value provided for the --auto-publish flag
	rootCmd.Flags().StringVar(&pathToTransaction, "auto-publish", defaultPathToTransaction, "auto publish a signed transaction when the network is upgradeable")

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
