package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/rpc/client/http"
)

var _ AppClient = &relayerClient{}

type relayerClient struct {
	tendermintRPC *http.HTTP
	logger        tmlog.Logger
	querier       Querier
	mutex         *sync.Mutex // TODO check if we need the mutex here
	evmClient     EVMClient
}

func NewRelayerClient(
	logger tmlog.Logger,
	tendermintRPC string,
	querier Querier,
	evmClient EVMClient,
) (AppClient, error) {
	trpc, err := http.New(tendermintRPC, "/websocket")
	if err != nil {
		return nil, err
	}
	err = trpc.Start()
	if err != nil {
		return nil, err
	}

	return &relayerClient{
		tendermintRPC: trpc,
		logger:        logger,
		querier:       querier,
		mutex:         &sync.Mutex{},
		evmClient:     evmClient,
	}, nil
}

func (oc *relayerClient) SubscribeValset(ctx context.Context) (<-chan types.Valset, error) {
	valsetsChan := make(chan types.Valset, 10)

	go func() {
		defer close(valsetsChan)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				lastContractNonce, err := oc.evmClient.StateLastValsetNonce(&bind.CallOpts{})
				if err != nil {
					oc.logger.Error(err.Error())
					continue
				}
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

				// If the contract has already the last version, no need to relay anything
				if lastContractNonce >= valset.Nonce {
					time.Sleep(10 * time.Second)
					continue
				}

				// we're incrementing by 1 since we still don't support heights
				// instead of nonce: https://github.com/celestiaorg/quantum-gravity-bridge/issues/104
				newVs, err := oc.querier.QueryValsetByNonce(ctx, lastContractNonce+1)
				if err != nil {
					oc.logger.Error(err.Error())
					continue
				}
				valsetsChan <- *newVs
				// Give some time for newVs to be committed before we continue
				// This will change with the worker pool design pattern we will adopt
				time.Sleep(time.Minute)
			}
		}
	}()

	return valsetsChan, nil
}

func (oc *relayerClient) SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error) {
	dataCommitments := make(chan ExtendedDataCommitment)

	go func() {
		defer close(dataCommitments)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				lastContractNonce, err := oc.evmClient.StateLastDataRootTupleRootNonce(&bind.CallOpts{})
				if err != nil {
					oc.logger.Error(err.Error())
					continue
				}

				currentHeight, err := oc.querier.QueryHeight(ctx)
				if err != nil {
					oc.logger.Error(err.Error())
					continue
				}
				currentNonce := currentHeight / types.DataCommitmentWindow

				// If we're at the latest nonce, we sleep
				if lastContractNonce >= currentNonce {
					time.Sleep(10 * time.Second)
					continue
				}

				// TODO: calculate start height some other way that can handle changes
				// in the data window param
				startHeight := lastContractNonce * types.DataCommitmentWindow
				endHeight := (lastContractNonce + 1) * types.DataCommitmentWindow

				// create and send the data commitment
				dcResp, err := oc.tendermintRPC.DataCommitment(
					ctx,
					fmt.Sprintf("block.height >= %d AND block.height <= %d",
						startHeight,
						endHeight,
					),
				)
				if err != nil {
					oc.logger.Error(err.Error())
					continue
				}

				// TODO: store the nonce in the state somewhere, so that we don't have
				// to assume what the nonce is
				nonce := lastContractNonce + 1

				dataCommitments <- ExtendedDataCommitment{
					Commitment: dcResp.DataCommitment,
					Start:      startHeight,
					End:        endHeight,
					Nonce:      nonce,
				}
				time.Sleep(time.Minute)
			}
		}

	}()

	return dataCommitments, nil
}
