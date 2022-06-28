package orchestrator

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	tmlog "github.com/tendermint/tendermint/libs/log"
)

func DeployCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "deploy <flags>",
		Short: "Deploys the QGB contract and initializes it using the provided Celestia chain",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseDeployFlags(cmd)
			if err != nil {
				return err
			}

			logger := tmlog.NewTMLogger(os.Stdout)

			querier, err := NewQuerier(config.celesGRPC, config.tendermintRPC, logger, MakeEncodingConfig())
			if err != nil {
				return err
			}

			// TODO change to get the current valaset
			// get the first valset
			vs, err := querier.QueryValsetByNonce(cmd.Context(), 1)
			if err != nil {
				return fmt.Errorf(
					"cannot initialize the QGB contract without having a valset request: %s",
					err.Error(),
				)
			}

			evmClient := NewEvmClient(
				tmlog.NewTMLogger(os.Stdout),
				nil,
				config.privateKey,
				config.evmRPC,
			)

			// the deploy QGB contract will handle the logging of the address
			_, _, _, err = evmClient.DeployQGBContract(
				cmd.Context(),
				*vs,
				0,
				config.evmChainID,
				true,
				false,
			)
			if err != nil {
				return err
			}

			querier.Stop()
			return nil
		},
	}
	return addDeployFlags(command)
}
