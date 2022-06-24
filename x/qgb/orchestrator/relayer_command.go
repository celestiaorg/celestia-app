package orchestrator

import (
	"os"
	"sync"
	"time"

	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/ethclient"
	tmlog "github.com/tendermint/tendermint/libs/log"

	"github.com/spf13/cobra"
)

func RelayerCmd() *cobra.Command {
	command := &cobra.Command{
		Use: "relayer <flags>",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseRelayerFlags(cmd)
			if err != nil {
				return err
			}

			logger := tmlog.NewTMLogger(os.Stdout)

			ethClient, err := ethclient.Dial(config.evmRPC)
			if err != nil {
				return err
			}
			qgbWrapper, err := wrapper.NewQuantumGravityBridge(config.contractAddr, ethClient)
			if err != nil {
				return err
			}

			querier, err := NewQuerier(config.celesGRPC, config.tendermintRPC, logger, MakeEncodingConfig())
			if err != nil {
				return err
			}

			relay, err := NewRelayer(
				querier,
				NewEvmClient(
					tmlog.NewTMLogger(os.Stdout),
					*qgbWrapper,
					config.privateKey,
					config.evmRPC,
				),
				logger,
			)
			if err != nil {
				return err
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
						err = relay.processEvents(cmd.Context())
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
	return addRelayerFlags(command)
}
