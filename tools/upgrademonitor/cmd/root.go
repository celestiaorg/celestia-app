package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/tools/upgrademonitor/internal"
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

		if err := internal.ValidateTransaction(pathToTransaction); err != nil {
			return fmt.Errorf("invalid transaction: %v", err)
		}

		currentVersion, err := internal.QueryCurrentVersion(conn)
		if err != nil {
			return err
		}
		nextVersion := currentVersion + 1
		fmt.Printf("currentVersion: %v, nextVersion: %v\n", currentVersion, nextVersion)

		for {
			select {
			case <-ticker.C:
				resp, err := internal.QueryVersionTally(conn, nextVersion)
				if err != nil {
					return err
				}
				fmt.Printf("nextVersion: %v, voting: %v, threshold: %v, total: %v\n", nextVersion, resp.GetVotingPower(), resp.GetThresholdPower(), resp.GetTotalVotingPower())

				if internal.IsUpgradeable(resp) {
					fmt.Printf("the network is upgradeable so publishing %v\n", pathToTransaction)
					resp, err := internal.Publish(conn, pathToTransaction)
					if err != nil {
						return err
					}
					if resp.Code != 0 {
						return fmt.Errorf("failed to publish transaction: %v\n", resp.RawLog)
					}
					fmt.Printf("published transaction: %v\n", resp.TxHash)
					return nil // stop the upgrademonitor
				}
			}
		}
	},
}

func Execute() {
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
