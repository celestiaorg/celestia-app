package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	// import celestia-app for it's side-effects so that celestia-app init()
	// overrides the Cosmos SDK config with the correct account address
	// prefixes.
	"github.com/celestiaorg/celestia-app/app"
	_ "github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/tools/upgrademonitor/internal"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

				if internal.IsUpgradeable(resp) {
					fmt.Printf("the network is upgradeable so attempting to publish %v\n", autoPublish)

					signedTx, err := os.ReadFile(autoPublish)
					if err != nil {
						return fmt.Errorf("failed to read file %v. %v", autoPublish, err)
					}

					encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
					transaction, err := encCfg.TxConfig.TxJSONDecoder()(signedTx)
					if err != nil {
						return fmt.Errorf("failed to unmarshal transaction: %v", err)
					}

					txBytes, err := encCfg.TxConfig.TxEncoder()(transaction)
					if err != nil {
						return fmt.Errorf("failed to encode transaction: %v", err)
					}

					conn, err := grpc.Dial(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
					if err != nil {
						return fmt.Errorf("failed to connect to GRPC server: %v", err)
					}
					defer conn.Close()

					client := tx.NewServiceClient(conn)
					res, err := client.BroadcastTx(context.Background(), &txTypes.BroadcastTxRequest{
						Mode:    tx.BroadcastMode_BROADCAST_MODE_BLOCK,
						TxBytes: txBytes,
					})
					if err != nil {
						return fmt.Errorf("failed to broadcast transaction: %v", err)
					}

					fmt.Printf("Broadcast transaction response: %+v", res.TxResponse)
					return nil
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
	// Bind the autoPublish variable to the --auto-publish flag
	rootCmd.Flags().StringVar(&autoPublish, "auto-publish", defaultAutoPublish, "auto publish a signed transaction when the network is upgradeable")

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
