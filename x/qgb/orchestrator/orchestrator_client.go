package orchestrator

import (
	"context"
	"fmt"
	"sync"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/rpc/client/http"
	coretypes "github.com/tendermint/tendermint/types"
)

var _ AppClient = &orchestratorClient{}

type orchestratorClient struct {
	tendermintRPC       *http.HTTP
	logger              tmlog.Logger
	querier             Querier
	mutex               *sync.Mutex // TODO check if we need the mutex here
	orchestratorAddress string
}

func NewOrchestratorClient(logger tmlog.Logger, tendermintRpc string, querier Querier, orchAddr string) (AppClient, error) {
	trpc, err := http.New(tendermintRpc, "/websocket")
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
	valsetsChan := make(chan types.Valset, 10)

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

				valset := valsets[0]

				// Checking if we already signed this valset
				resp, err := oc.querier.QueryValsetConfirm(ctx, valset.Nonce, oc.orchestratorAddress)
				if err != nil {
					oc.logger.Error(err.Error())
					return
				}
				if resp == nil && !contains(nonces, valset.Nonce) {
					nonces = append(nonces, valset.Nonce)
					valsetsChan <- valset
				}
			}
		}
	}()

	return valsetsChan, nil
}

func (oc *orchestratorClient) SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error) {
	dataCommitments := make(chan ExtendedDataCommitment)

	// queryClient := types.NewQueryClient(orchestratorClient.qgbRPC)

	// resp, err := queryClient.Params(ctx, &types.QueryParamsRequest{})
	// if err != nil {
	// 	return nil, err
	// }

	// params := resp.Params
	q := coretypes.EventQueryNewBlockHeader.String()
	results, err := oc.tendermintRPC.Subscribe(ctx, "height", q)
	if err != nil {
		return nil, err
	}

	go func() {
		defer close(dataCommitments)

		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-results:
				eventDataHeader := ev.Data.(coretypes.EventDataNewBlockHeader)
				height := eventDataHeader.Header.Height
				// todo: refactor to ensure that no ranges of blocks are missed if the
				// parameters are changed
				if height%int64(types.DataCommitmentWindow) != 0 {
					continue
				}

				// TODO: calculate start height some other way that can handle changes
				// in the data window param
				startHeight := height - int64(types.DataCommitmentWindow)
				endHeight := height

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
				nonce := uint64(height) / types.DataCommitmentWindow

				dataCommitments <- ExtendedDataCommitment{
					Commitment: dcResp.DataCommitment,
					Start:      startHeight,
					End:        endHeight,
					Nonce:      nonce,
				}

			}
		}

	}()

	return dataCommitments, nil
}
