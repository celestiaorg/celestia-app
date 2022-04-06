package orchestrator

import (
	tmlog "github.com/tendermint/tendermint/libs/log"
	"os"
	"strings"
	"sync"
	"time"

	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func OrchestratorCmd() *cobra.Command {
	command := &cobra.Command{
		Use:     "orchestrator <flags>",
		Aliases: []string{"orch"},
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseOrchestratorFlags(cmd)
			if err != nil {
				return err
			}

			// open a keyring using the configured settings
			// TODO: optionally ask for input for a password
			ring, err := keyring.New("orchestrator", config.keyringBackend, config.keyringAccount, strings.NewReader(""))
			if err != nil {
				return err
			}

			client, err := NewAppClient(
				tmlog.NewTMLogger(os.Stdout),
				config.keyringAccount,
				config.celestiaChainID,
				config.tendermintRPC,
				config.qgbRPC,
			)
			if err != nil {
				return err
			}

			orch := orchestrator{
				logger:    zerolog.New(os.Stdout),
				appClient: client,
				signer: paytypes.NewKeyringSigner(
					ring,
					config.keyringAccount,
					config.celestiaChainID,
				),
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
						valsetChan, err := client.SubscribeValset(ctx)
						err = orch.processValsetEvents(ctx, valsetChan)
						if err != nil {
							orch.logger.Err(err)
							// todo: refactor to make a more sophisticated retry mechanism
							time.Sleep(time.Second * 30)
							continue
						}
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
						err = orch.processDataCommitmentEvents(ctx, dcChan)
						if err != nil {
							orch.logger.Err(err)
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
