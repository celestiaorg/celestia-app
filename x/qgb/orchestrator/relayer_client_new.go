package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
)

func (oc *relayerClient) SubscribeValset1(ctx context.Context) (<-chan types.Valset, error) {
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
				latestNonce, err := oc.querier.QueryLatestAttestationNonce(ctx)

				// If the contract has already the last version, no need to relay anything
				if lastContractNonce >= latestNonce {
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
				time.Sleep(10 * time.Second)
			}
		}
	}()

	return valsetsChan, nil
}

func (oc *relayerClient) SubscribeDataCommitment1(ctx context.Context) (<-chan ExtendedDataCommitment, error) {
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

				latestNonce, err := oc.querier.QueryLatestAttestationNonce(ctx)
				if err != nil {
					oc.logger.Error(err.Error())
					time.Sleep(5 * time.Second)
					continue
				}

				// If we're at the latest nonce, we sleep
				if lastContractNonce >= latestNonce {
					time.Sleep(10 * time.Second)
					continue
				}

				// query data commitment request
				dc, err := oc.querier.QueryDataCommitmentByNonce(ctx, lastContractNonce+1)
				if err != nil {
					oc.logger.Error(err.Error())
					time.Sleep(5 * time.Second)
					continue
				}
				if dc == nil {
					time.Sleep(5 * time.Second)
					continue
				}

				// create and send the data commitment
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

				dataCommitments <- ExtendedDataCommitment{
					Commitment: dcResp.DataCommitment,
					Data: *types.NewDataCommitment(
						dc.Nonce,
						dc.BeginBlock,
						dc.EndBlock,
					),
				}
				time.Sleep(10 * time.Second)
			}
		}

	}()

	return dataCommitments, nil
}
