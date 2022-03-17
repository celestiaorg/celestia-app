package orchestrator

import (
	"os"
	"strings"
	"sync"
	"time"

	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func RelayerCmd() *cobra.Command {
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
			ring, err := keyring.New("orchestrator", config.keyringBackend, config.keyringAccount, strings.NewReader("."))
			if err != nil {
				return err
			}

			client, err := newClient(
				zerolog.New(os.Stdout),
				paytypes.NewKeyringSigner(ring, config.keyringAccount, config.celestiaChainID),
				config,
			)
			if err != nil {
				return err
			}

			relay := relayer{client: client} // todo add wrappers

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
