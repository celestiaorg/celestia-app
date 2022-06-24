package orchestrator

import (
	"context"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/crypto"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/types"

	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"

	tmlog "github.com/tendermint/tendermint/libs/log"

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

			querier, err := NewQuerier(config.celesGRPC, config.tendermintRPC, logger, MakeEncodingConfig())
			if err != nil {
				return err
			}

			client, err := NewOrchestratorClient(
				logger,
				config.tendermintRPC,
				querier,
				signer.GetSignerInfo().GetAddress().String(),
			)
			if err != nil {
				return err
			}

			broadcaster, err := NewBroadcaster(config.celesGRPC, signer)
			if err != nil {
				return nil
			}

			orchEthAddr, err := stakingtypes.NewEthAddress(crypto.PubkeyToAddress(config.privateKey.PublicKey).Hex())
			if err != nil {
				return err
			}

			orch := orchestrator{
				broadcaster:         broadcaster,
				evmPrivateKey:       *config.privateKey,
				bridgeID:            types.BridgeId,
				orchestratorAddress: signer.GetSignerInfo().GetAddress(),
				orchEthAddress:      *orchEthAddr,
				logger:              logger,
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
						ctx, cancel := context.WithCancel(ctx)
						valsetChan, err := client.SubscribeValset(ctx)
						if err != nil {
							cancel()
							logger.Error(err.Error())
							time.Sleep(time.Second * 30)
							continue
						}
						err = orch.processValsetEvents(ctx, valsetChan)
						if err != nil {
							cancel()
							logger.Error(err.Error())
							// todo: refactor to make a more sophisticated retry mechanism
							time.Sleep(time.Second * 30)
							continue
						}
						cancel()
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
						ctx, cancel := context.WithCancel(ctx)
						dcChan, err := client.SubscribeDataCommitment(ctx)
						if err != nil {
							cancel()
							logger.Error(err.Error())
							time.Sleep(time.Second * 30)
							continue
						}
						err = orch.processDataCommitmentEvents(ctx, dcChan)
						if err != nil {
							cancel()
							logger.Error(err.Error())
							time.Sleep(time.Second * 30)
							continue
						}
						cancel()
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
