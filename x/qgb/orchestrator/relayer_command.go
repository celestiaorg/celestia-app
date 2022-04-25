package orchestrator

import (
	"fmt"
	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"os"
	"strings"
	"sync"
	"time"

	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/ethclient"
	tmlog "github.com/tendermint/tendermint/libs/log"

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

			logger := tmlog.NewTMLogger(os.Stdout)

			// TODO the relayer doesn't need the signer
			ethClient, err := ethclient.Dial(config.evmRPC)
			if err != nil {
				return err
			}
			qgbWrapper, err := wrapper.NewQuantumGravityBridge(config.contractAddr, ethClient)
			if err != nil {
				return err
			}

			querier, err := NewQuerier(config.qgbRPC, config.tendermintRPC, logger)
			if err != nil {
				return err
			}

			client, err := NewRelayerClient(
				logger,
				config.tendermintRPC,
				querier,
				signer.GetSignerInfo().GetAddress().String(),
				NewEvmClient(
					tmlog.NewTMLogger(os.Stdout),
					*qgbWrapper,
					config.privateKey,
					config.evmRPC,
				),
			)
			if err != nil {
				return err
			}

			// TODO add NewRelayer method
			relay := relayer{
				bridgeID: types.BridgeId,
				evmClient: NewEvmClient(
					tmlog.NewTMLogger(os.Stdout),
					*qgbWrapper,
					config.privateKey,
					config.evmRPC,
				),
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
						if err != nil {
							// TODO is this the correct way ?
							logger.Error(err.Error())
							return
						}
						err = relay.processValsetEvents(cmd.Context(), valsetChan)
						if err != nil {
							logger.Error(err.Error())
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
						dcChan, err := client.SubscribeDataCommitment(cmd.Context())
						if err != nil {
							// TODO is this the correct way ?
							fmt.Println(err.Error())
							return
						}
						err = relay.processDataCommitmentEvents(cmd.Context(), dcChan)
						if err != nil {
							logger.Error(err.Error())
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
