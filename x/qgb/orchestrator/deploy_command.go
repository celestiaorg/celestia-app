package orchestrator

import (
	"fmt"
	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"math/big"
	"os"
	"strings"

	"github.com/spf13/cobra"
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

			// creates the signer
			//TODO: optionally ask for input for a password
			ring, err := keyring.New("orchestrator", config.keyringBackend, config.keyringPath, strings.NewReader(""))
			if err != nil {
				return err
			}
			signer := paytypes.NewKeyringSigner(
				ring,
				config.keyringAccount,
				config.celestiaChainID,
			)

			// TODO the deployer doesn't need the signer
			client, err := NewAppClient(
				tmlog.NewTMLogger(os.Stdout),
				signer,
				config.celestiaChainID,
				config.tendermintRPC,
				config.qgbRPC,
			)
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
			var bridgeId [32]byte
			copy(bridgeId[:], types.BridgeId.Bytes()) // is this safe?

			// get latest valset
			lastValset, err := client.QueryLastValsets(cmd.Context())
			if err != nil {
				return fmt.Errorf(
					"Cannot initialize the QGB contract without having a valset request: %s",
					err.Error(),
				)
			}

			ethVsHash, err := lastValset[0].Hash()
			if err != nil {
				return err
			}

			// deploy the QGB contract using the chain parameters
			addr, tx, _, err := wrapper.DeployQuantumGravityBridge(
				auth,
				ethClient,
				bridgeId,
				big.NewInt(int64(lastValset[0].TwoThirdsThreshold())),
				ethVsHash,
			)
			if err != nil {
				return err
			}
			fmt.Printf("QGB contract deployed successfuly.\n- Transaction hash: %s\n- Contract address: %s\n", tx.Hash(), addr.Hex())
			return nil
		},
	}
	return addOrchestratorFlags(command)
}
