package orchestrator

import (
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
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

			client, err := newClient(
				zerolog.New(os.Stdout),
				config,
			)
			if err != nil {
				return err
			}

			relay := relayer{client: client}

			wg := &sync.WaitGroup{}

			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-cmd.Context().Done():
						return
					default:
						err = relay.relayValsets(cmd.Context())
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
