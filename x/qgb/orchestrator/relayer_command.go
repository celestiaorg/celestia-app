package orchestrator

import (
	"github.com/rs/zerolog"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

func RelayerCmd() *cobra.Command {
	command := &cobra.Command{
		Use: "relayer <flags>",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseOrchestratorFlags(cmd)
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

			relay := relayer{
				logger:    zerolog.New(os.Stdout),
				appClient: client,
			}

			wg := &sync.WaitGroup{}

			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-cmd.Context().Done():
						return
					default:
						valsetChan, err := client.SubscribeValset(cmd.Context())
						err = relay.processValsetEvents(cmd.Context(), valsetChan)
						if err != nil {
							relay.logger.Err(err)
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
					case <-cmd.Context().Done():
						return
					default:
						err = relay.relayDataCommitments(cmd.Context())
						if err != nil {
							relay.logger.Err(err)
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
