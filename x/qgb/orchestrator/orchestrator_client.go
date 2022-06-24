package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
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

// TODO this will be removed when we use the new job/worker design for the client.
func contains(s []uint64, nonce uint64) bool {
	for _, v := range s {
		if v == nonce {
			return true
		}
	}
	return false
}

var nonces = make([]uint64, 10000)

func (oc *orchestratorClient) SubscribeValset(ctx context.Context) (<-chan types.Valset, error) {
	valsetsChan := make(chan types.Valset, 100)

	// will change once we have the new design
	go oc.addOldValsetAttestations(ctx, valsetsChan)

	latestNonce, err := oc.querier.QueryLatestAttestationNonce(ctx)
	if err != nil {
		oc.logger.Error(err.Error())
		time.Sleep(1 * time.Second)
	}
	if latestNonce == 0 {
		latestNonce++
	}
	go func() {
		defer close(valsetsChan)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				valset, err := oc.querier.QueryValsetByNonce(ctx, latestNonce)
				if err != nil {
					if errors.Is(err, types.ErrAttestationNotValsetRequest) {
						latestNonce++
						continue
					}
					time.Sleep(1 * time.Second)
					continue
				}

				// Checking if we already signed this valset
				resp, err := oc.querier.QueryValsetConfirm(ctx, valset.Nonce, oc.orchestratorAddress)
				if err != nil {
					oc.logger.Error(err.Error())
					time.Sleep(1 * time.Second)
					continue
				}

				if resp == nil && !contains(nonces, valset.Nonce) {
					valsetsChan <- *valset
					nonces = append(nonces, valset.Nonce)
				}
				latestNonce++
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

	latestNonce, err := oc.querier.QueryLatestAttestationNonce(ctx)
	if err != nil {
		oc.logger.Error(err.Error())
		return
	}

	previousNonce := latestNonce - 1
	for previousNonce < latestNonce {
		if previousNonce == 0 {
			break
		}
		lastVsConfirm, err := oc.querier.QueryValsetConfirm(ctx, previousNonce, oc.orchestratorAddress)
		if err != nil {
			oc.logger.Error(err.Error())
			time.Sleep(1 * time.Second)
			continue
		}
		// The valset signed by the orchestrator to get lastVsConfirm
		// Used to get the height that valset was first introduced
		correspondingVs, err := oc.querier.QueryValsetByNonce(ctx, previousNonce)
		if err != nil {
			if !errors.Is(err, types.ErrAttestationNotValsetRequest) {
				oc.logger.Error(fmt.Sprintf("nonce %d: %s", previousNonce, err.Error()))
			}
			previousNonce--
			continue
		}
		if correspondingVs.Height < lastUnbondingHeight {
			// Most likely, we're up to date and don't need to catchup anymore
			return
		}
		if lastVsConfirm != nil {
			// in case we have holes in the signatures
			previousNonce--
			continue
		}

		// valsetChan is the ordinary valset channel used above. The orchestrator keeps adding to it
		// old attestations same as with new ones when listening.
		if !contains(nonces, correspondingVs.Nonce) {
			valsetsChan <- *correspondingVs
			nonces = append(nonces, correspondingVs.Nonce)
		}
		previousNonce--
	}
}

func (oc *orchestratorClient) SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error) {
	dataCommitments := make(chan ExtendedDataCommitment, 100)

	// will change once we have the new design
	go oc.addOldDataCommitmentAttestations(ctx, dataCommitments) //nolint:errcheck

	nonces := make([]uint64, 10000)

	// TODO retry
	latestNonce, err := oc.querier.QueryLatestAttestationNonce(ctx)
	if err != nil {
		oc.logger.Error(err.Error())
	}
	if latestNonce == 0 {
		latestNonce++
	}

	go func() {
		defer close(dataCommitments)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				// query data commitment request
				dc, err := oc.querier.QueryDataCommitmentByNonce(ctx, latestNonce)
				if err != nil {
					if errors.Is(err, types.ErrAttestationNotDataCommitmentRequest) {
						latestNonce++
					}
					time.Sleep(1 * time.Second)
					continue
				}
				if dc == nil {
					time.Sleep(1 * time.Second)
					continue
				}

				// check if already signed
				signed, err := oc.querier.QueryDataCommitmentConfirm(ctx, dc.EndBlock, dc.BeginBlock, oc.orchestratorAddress)
				if err != nil {
					oc.logger.Error(err.Error())
					time.Sleep(1 * time.Second)
					continue
				}
				if signed != nil {
					time.Sleep(1 * time.Second)
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
					time.Sleep(1 * time.Second)
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
				latestNonce++
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

	latestNonce, err := oc.querier.QueryLatestAttestationNonce(ctx)
	if err != nil {
		oc.logger.Error(err.Error())
		return
	}

	n := uint64(0)
	for n <= latestNonce {

		// To start signing from new to old
		nonce := latestNonce - n

		// query data commitment request
		dc, err := oc.querier.QueryDataCommitmentByNonce(ctx, nonce)
		if err != nil {
			if !errors.Is(err, types.ErrAttestationNotDataCommitmentRequest) {
				oc.logger.Error(fmt.Sprintf("dc nonce %d: %s", nonce, err.Error()))
			}
			n++
			continue
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
			time.Sleep(1 * time.Second)
			continue
		}

		if dc.EndBlock < lastUnbondingHeight {
			// Most likely, we're up to date and don't need to catchup anymore
			return
		}
		if existingConfirm != nil {
			// In case we have holes in the signatures
			n++
			time.Sleep(1 * time.Second)
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
			time.Sleep(1 * time.Second)
			continue
		}

		dataCommitmentsChan <- ExtendedDataCommitment{
			Commitment: previousCommitment.DataCommitment,
			Data:       *dc,
		}
		n++
	}
}
