package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/rpc/client/http"
)

var _ AppClient = &orchestratorClient{}

type orchestratorClient struct {
	tendermintRPC       *http.HTTP
	logger              tmlog.Logger
	querier             Querier
	mutex               *sync.Mutex // TODO check if we need the mutex here
	orchestratorAddress string
}

func NewOrchestratorClient(
	logger tmlog.Logger,
	tendermintRPC string,
	querier Querier,
	orchAddr string,
) (AppClient, error) {
	trpc, err := http.New(tendermintRPC, "/websocket")
	if err != nil {
		return nil, err
	}
	err = trpc.Start()
	if err != nil {
		return nil, err
	}

	return &orchestratorClient{
		tendermintRPC:       trpc,
		logger:              logger,
		querier:             querier,
		mutex:               &sync.Mutex{},
		orchestratorAddress: orchAddr,
	}, nil
}

// TODO this will be removed when we use the new job/worker design for the client
func contains(s []uint64, nonce uint64) bool {
	for _, v := range s {
		if v == nonce {
			return true
		}
	}
	return false
}

func (oc *orchestratorClient) SubscribeValset(ctx context.Context) (<-chan types.Valset, error) {
	valsetsChan := make(chan types.Valset, 100)

	// will change once we have the new design
	go oc.addOldValsetAttestations(ctx, valsetsChan)

	results, err := oc.tendermintRPC.Subscribe(
		ctx,
		"valset-changes",
		fmt.Sprintf("%s.%s='%s'", types.EventTypeValsetRequest, sdk.AttributeKeyModule, types.ModuleName),
	)
	if err != nil {
		return nil, err
	}

	nonces := make([]uint64, 10000)

	go func() {
		defer close(valsetsChan)
		for {
			select {
			case <-ctx.Done():
				return
			case <-results:
				// TODO add query for LatestValsetNonce and use it instead of this
				valsets, err := oc.querier.QueryLastValsets(ctx)
				if err != nil {
					oc.logger.Error(err.Error())
					continue
				}

				// todo: double check that the first validator set is found
				if len(valsets) < 1 {
					oc.logger.Error("no validator sets found")
					continue
				}

				valset := valsets[0]

				// Checking if we already signed this valset
				resp, err := oc.querier.QueryValsetConfirm(ctx, valset.Nonce, oc.orchestratorAddress)
				if err != nil {
					oc.logger.Error(err.Error())
					continue
				}

				if resp == nil && !contains(nonces, valset.Nonce) {
					valsetsChan <- valset
					nonces = append(nonces, valset.Nonce)
				}
			}
		}
	}()

	return valsetsChan, nil
}

func (oc *orchestratorClient) addOldValsetAttestations(ctx context.Context, valsetsChan chan types.Valset) {
	oc.logger.Info("Started adding Valsets attestation to queue")
	defer oc.logger.Info("Finished adding Valsets attestation to queue")
	lastUnbondingHeight, err := oc.querier.QueryLastUnbondingHeight(ctx)
	if err != nil {
		oc.logger.Error(err.Error())
		return
	}

	// TODO add query for LatestValsetNonce and use it instead of this
	valsets, err := oc.querier.QueryLastValsets(ctx)
	if err != nil {
		oc.logger.Error(err.Error())
		return
	}

	// todo: double check that the first validator set is found
	if len(valsets) < 1 {
		oc.logger.Error("no validator sets found")
		return
	}
	valsetsChan <- valsets[0]

	previousNonce := valsets[0].Nonce
	for {
		if previousNonce == 1 {
			return
		}
		previousNonce = previousNonce - 1
		lastVsConfirm, err := oc.querier.QueryValsetConfirm(ctx, previousNonce, oc.orchestratorAddress)
		if err != nil {
			oc.logger.Error(err.Error())
			return
		}
		// The valset signed by the orchestrator to get lastVsConfirm
		// Used to get the height that valset was first introduced
		correspondingVs, err := oc.querier.QueryValsetByNonce(ctx, previousNonce)
		if err != nil {
			oc.logger.Error(err.Error())
			return
		}
		if correspondingVs.Height < lastUnbondingHeight {
			// Most likely, we're up to date and don't need to catchup anymore
			return
		}
		if lastVsConfirm != nil {
			// in case we have holes in the signatures
			continue
		}

		// valsetChan is the ordinary valset channel used above. The orchestrator keeps adding to it
		// old attestations same as with new ones when listening.
		valsetsChan <- *correspondingVs
	}
}

func (oc *orchestratorClient) SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error) {
	dataCommitments := make(chan ExtendedDataCommitment, 100)

	// will change once we have the new design
	go oc.addOldDataCommitmentAttestations(ctx, dataCommitments) //nolint:errcheck

	// queryClient := types.NewQueryClient(orchestratorClient.celesGRPC)

	// resp, err := queryClient.Params(ctx, &types.QueryParamsRequest{})
	// if err != nil {
	// 	return nil, err
	// }

	// params := resp.Params

	nonces := make([]uint64, 10000)

	go func() {
		defer close(dataCommitments)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				latestDCNonce, err := oc.querier.QueryLatestDataCommitmentNonce(ctx)
				if err != nil {
					oc.logger.Error(err.Error())
					time.Sleep(5 * time.Second)
					continue
				}

				// query data commitment request
				dc, err := oc.querier.QueryDataCommitmentByNonce(ctx, latestDCNonce)
				if err != nil {
					oc.logger.Error(err.Error())
					time.Sleep(5 * time.Second)
					continue
				}
				if dc == nil {
					time.Sleep(5 * time.Second)
					continue
				}

				// check if already signed
				signed, err := oc.querier.QueryDataCommitmentConfirm(ctx, dc.EndBlock, dc.BeginBlock, oc.orchestratorAddress)
				if err != nil {
					oc.logger.Error(err.Error())
					time.Sleep(5 * time.Second)
					continue
				}
				if signed != nil {
					time.Sleep(5 * time.Second)
					continue
				}

				// get the commitment
				dcResp, err := oc.tendermintRPC.DataCommitment(
					ctx,
					fmt.Sprintf("block.height >= %d AND block.height <= %d",
						dc.BeginBlock,
						dc.EndBlock,
					),
				)
				if err != nil {
					oc.logger.Error(err.Error())
					continue
				}
				if !contains(nonces, dc.Nonce) {
					dataCommitments <- ExtendedDataCommitment{
						Commitment: dcResp.DataCommitment,
						Data:       *dc,
					}
					nonces = append(nonces, dc.Nonce)
				}
				time.Sleep(5 * time.Second)
			}
		}
	}()

	return dataCommitments, nil
}

func (oc *orchestratorClient) addOldDataCommitmentAttestations(
	ctx context.Context,
	dataCommitmentsChan chan ExtendedDataCommitment,
) {
	oc.logger.Info("Started adding old Data Commitments attestation to queue")
	defer oc.logger.Info("Finished adding old Data Commitments attestation to queue")
	lastUnbondingHeight, err := oc.querier.QueryLastUnbondingHeight(ctx)
	if err != nil {
		oc.logger.Error(err.Error())
		return
	}

	latestDCNonce, err := oc.querier.QueryLatestDataCommitmentNonce(ctx)
	if err != nil {
		oc.logger.Error(err.Error())
		return
	}

	for n := uint64(0); n <= latestDCNonce; n++ {

		// To start signing from new to old
		nonce := latestDCNonce - n

		// query data commitment request
		dc, err := oc.querier.QueryDataCommitmentByNonce(ctx, nonce)
		if err != nil {
			oc.logger.Error(err.Error())
			return
		}
		if dc == nil {
			return
		}

		existingConfirm, err := oc.querier.QueryDataCommitmentConfirm(
			ctx,
			dc.EndBlock,
			dc.BeginBlock,
			oc.orchestratorAddress,
		)
		if err != nil {
			oc.logger.Error(err.Error())
			continue
		}

		if dc.EndBlock < lastUnbondingHeight {
			// Most likely, we're up to date and don't need to catchup anymore
			return
		}
		if existingConfirm != nil {
			// In case we have holes in the signatures
			continue
		}

		previousCommitment, err := oc.tendermintRPC.DataCommitment(
			ctx,
			fmt.Sprintf("block.height >= %d AND block.height <= %d",
				dc.BeginBlock,
				dc.EndBlock,
			),
		)
		if err != nil {
			oc.logger.Error(err.Error())
			continue
		}

		dataCommitmentsChan <- ExtendedDataCommitment{
			Commitment: previousCommitment.DataCommitment,
			Data:       *dc,
		}
	}
}
