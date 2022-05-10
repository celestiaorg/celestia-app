package orchestrator

import (
	"fmt"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/spf13/cobra"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"math/big"
	"os"
)

func DeployCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "deploy <flags>",
		Short: "Deploys the QGB contract and initializes it using the provided Celestia chain",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseOrchestratorFlags(cmd)
			if err != nil {
				return err
			}

			logger := tmlog.NewTMLogger(os.Stdout)

			// TODO make the deployer config only have the required params
			querier, err := NewQuerier(config.qgbRPC, config.tendermintRPC, logger)
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

			// get latest valset
			lastValset, err := querier.QueryLastValsets(cmd.Context())
			if err != nil {
				return fmt.Errorf(
					"cannot initialize the QGB contract without having a valset request: %s",
					err.Error(),
				)
			}

			ethVsHash, err := lastValset[0].Hash()
			if err != nil {
				return err
			}

			// deploy the QGB contract using the chain parameters
			// TODO move the deploy to the evm client
			addr, tx, _, err := wrapper.DeployQuantumGravityBridge(
				auth,
				ethClient,
				bridgeID,
				big.NewInt(int64(lastValset[0].TwoThirdsThreshold())),
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
			return nil
		},
	}
	return addOrchestratorFlags(command)
}
