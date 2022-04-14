package orchestrator

import (
	"fmt"
	"os"
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

			client, err := NewAppClient(
				tmlog.NewTMLogger(os.Stdout),
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

			ethClient, err := ethclient.Dial(config.evmRPC)
			if err != nil {
				return err
			}

			qgbWrapper, err := wrapper.NewQuantumGravityBridge(config.contractAddr, ethClient)
			if err != nil {
				return err
			}

			relay := relayer{
				logger:    tmlog.NewTMLogger(tmlog.NewSyncWriter(os.Stdout)),
				appClient: client,
				bridgeID:  config.bridgeID,
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
							fmt.Println(err.Error())
							return
						}
						err = relay.processValsetEvents(cmd.Context(), valsetChan)
						if err != nil {
							relay.logger.Error(err.Error())
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
							relay.logger.Error(err.Error())
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
