package orchestrator

import (
	tmlog "github.com/tendermint/tendermint/libs/log"
	"os"

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

			logger := tmlog.NewTMLogger(os.Stdout)

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
				orchestratorAddress: config.keyringAccount,
			}

			ctx := cmd.Context()
			err = NewOrchestratorEngine(
				ctx,
				orch,
				config.timeout,
				config.replay,
				config.follow,
			).Start()
			if err != nil {
				return err
			}
			return nil
		},
	}
	return addOrchestratorFlags(command)
}
