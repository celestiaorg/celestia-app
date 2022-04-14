package orchestrator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"

	tmlog "github.com/tendermint/tendermint/libs/log"

	"github.com/spf13/cobra"
)

func OrchestratorCmd() *cobra.Command {
	command := &cobra.Command{
		Use:     "orchestrator <flags>",
		Aliases: []string{"orch"},
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseOrchestratorFlags(cmd)
			//clientCtx, err := cosmosclient.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			logger := tmlog.NewTMLogger(os.Stdout)

			ring, err := keyring.New("orchestrator", config.keyringBackend, config.keyringPath, strings.NewReader(""))
			if err != nil {
				return err
			}

			signer := paytypes.NewKeyringSigner(
				ring,
				config.keyringAccount,
				config.celestiaChainID,
			)

			//fmt.Println(clientCtx.GetFromAddress())
			//signer := types.NewKeyringSigner(clientCtx.Keyring, "validator1", config.celestiaChainID)
			client, err := NewAppClient(
				logger,
				config.keyringAccount,
				config.keyringBackend,
				config.keyringPath,
				config.celestiaChainID,
				config.tendermintRPC,
				config.qgbRPC,
			)
			if err != nil {
				return err
			}

			orch := orchestrator{
				logger:              tmlog.NewTMLogger(tmlog.NewSyncWriter(os.Stdout)),
				appClient:           client,
				evmPrivateKey:       *config.privateKey,
				bridgeID:            config.bridgeID,
				orchestratorAddress: signer.GetSignerInfo().GetAddress().String(),
			}

			wg := &sync.WaitGroup{}
			ctx := cmd.Context()

			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-ctx.Done():
						return
					default:
						ctx, cancel := context.WithCancel(ctx)
						valsetChan, err := client.SubscribeValset(ctx)
						if err != nil {
							cancel()
							logger.Error(err.Error())
							time.Sleep(time.Second * 30)
							continue
						}
						err = orch.processValsetEvents(ctx, valsetChan)
						if err != nil {
							cancel()
							logger.Error(err.Error())
							// todo: refactor to make a more sophisticated retry mechanism
							time.Sleep(time.Second * 30)
							continue
						}
						cancel()
						return
					}
				}

			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-ctx.Done():
						return
					default:
						dcChan, err := client.SubscribeDataCommitment(ctx)
						if err != nil {
							fmt.Println(err.Error())
							return
						}
						err = orch.processDataCommitmentEvents(ctx, dcChan)
						if err != nil {
							logger.Error(err.Error())
							time.Sleep(time.Second * 30)
							continue
						}
						return
					}
				}

			}()

			wg.Wait()

			return nil
		},
	}
	return addOrchestratorFlags(command)
}
