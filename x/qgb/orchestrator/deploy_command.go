package orchestrator

import (
	"context"
	"os"
	"strconv"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/types/errors"
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

			encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

			querier, err := NewQuerier(config.celesGRPC, config.tendermintRPC, logger, encCfg)
			if err != nil {
				return err
			}

			vs, err := getStartingValset(cmd.Context(), querier, config.startingNonce)
			if err != nil {
				return errors.Wrap(
					err,
					"cannot initialize the QGB contract without having a valset request: %s",
				)
			}

			evmClient := NewEvmClient(
				tmlog.NewTMLogger(os.Stdout),
				nil,
				config.privateKey,
				config.evmRPC,
				config.evmGasLimit,
			)

			// the deploy QGB contract will handle the logging of the address
			_, _, _, err = evmClient.DeployQGBContract(
				cmd.Context(),
				*vs,
				vs.Nonce,
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

func getStartingValset(ctx context.Context, q *querier, snonce string) (*types.Valset, error) {
	switch snonce {
	case "latest":
		return q.QueryLatestValset(ctx)
	case "earliest":
		return q.QueryValsetByNonce(ctx, 1)
	default:
		nonce, err := strconv.ParseUint(snonce, 10, 0)
		if err != nil {
			return nil, err
		}
		attestation, err := q.QueryAttestationByNonce(ctx, nonce)
		if err != nil {
			return nil, err
		}
		if attestation == nil {
			return nil, types.ErrNilAttestation
		}
		if attestation.Type() == types.ValsetRequestType {
			value, ok := attestation.(*types.Valset)
			if !ok {
				return nil, ErrUnmarshallValset
			}
			return value, nil
		}
		return q.QueryLastValsetBeforeNonce(ctx, nonce)
	}
}
