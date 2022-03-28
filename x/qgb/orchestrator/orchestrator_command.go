package orchestrator

import (
	"log"
	"os"
	"strings"
	"sync"
	"time"

	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var (
	HomeDir string
)

func init() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	HomeDir = homeDir
}

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

			client, err := newClient(
				zerolog.New(os.Stdout),
				config,
			)
			if err != nil {
				return err
			}

			orch := orchestrator{
				client: client,
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
						err = orch.orchestrateValsets(ctx)
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
						err = orch.orchestrateDataCommitments(ctx)
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
