package orchestrator

import (
	"fmt"
	"math/big"
	"os"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
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

			querier, err := NewQuerier(config.celesGRPC, config.tendermintRPC, logger)
			if err != nil {
				return err
			}

			// init ethClient
			ethClient, err := ethclient.Dial(config.evmRPC)
			if err != nil {
				return err
			}

			// init evm account
			auth, err := bind.NewKeyedTransactorWithChainID(config.privateKey, big.NewInt(int64(config.evmChainID)))
			if err != nil {
				return err
			}

			// init bridgeID
			var bridgeID [32]byte
			copy(bridgeID[:], types.BridgeId.Bytes()) // is this safe?

			// get the first valset
			vs, err := querier.QueryValsetByNonce(cmd.Context(), 1)
			if err != nil {
				return fmt.Errorf(
					"cannot initialize the QGB contract without having a valset request: %s",
					err.Error(),
				)
			}

			ethVsHash, err := vs.Hash()
			if err != nil {
				return err
			}

			// deploy the QGB contract using the chain parameters
			// TODO move the deploy to the evm client
			addr, tx, _, err := wrapper.DeployQuantumGravityBridge(
				auth,
				ethClient,
				bridgeID,
				big.NewInt(int64(vs.TwoThirdsThreshold())),
				ethVsHash,
			)
			if err != nil {
				return err
			}
			fmt.Printf(
				"QGB contract deployed successfully.\n- Transaction hash: %s\n- Contract address: %s\n",
				tx.Hash(),
				addr.Hex(),
			)
			querier.Stop()
			return nil
		},
	}
	return addDeployFlags(command)
}
