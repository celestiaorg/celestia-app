package orchestrator

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/tendermint/tendermint/libs/bytes"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/rpc/client/http"
	coretypes "github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"
)

type AppClient interface {
	SubscribeValset(ctx context.Context) (<-chan types.Valset, error)
	SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error)
}

type appClient struct {
	tendermintRPC *http.HTTP
	qgbRPC        *grpc.ClientConn
	logger        tmlog.Logger
}

func NewAppClient(logger tmlog.Logger, coreRPC, appRPC string) (AppClient, error) {
	trpc, err := http.New(coreRPC, "/websocket")
	if err != nil {
		return nil, err
	}

	qgbGRPC, err := grpc.Dial(appRPC, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	return &appClient{
		tendermintRPC: trpc,
		qgbRPC:        qgbGRPC,
		logger:        logger,
	}, nil
}

func (ac *appClient) SubscribeValset(ctx context.Context) (<-chan types.Valset, error) {
	valsets := make(chan types.Valset, 10)
	results, err := ac.tendermintRPC.Subscribe(ctx, "valset-changes", "tm.event='Tx' AND message.module='qgb'")
	if err != nil {
		return nil, err
	}

	go func() {
		defer close(valsets)
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-results:
				attributes := ev.Events[types.EventTypeValsetRequest]
				for _, attr := range attributes {
					if attr != types.AttributeKeyNonce {
						continue
					}

					queryClient := types.NewQueryClient(ac.qgbRPC)

					lastValsetResp, err := queryClient.LastValsetRequests(ctx, &types.QueryLastValsetRequestsRequest{})
					if err != nil {
						ac.logger.Error(err.Error())
						return
					}

					// todo: double check that the first validator set is found
					if len(lastValsetResp.Valsets) < 1 {
						ac.logger.Error("no validator sets found")
						return
					}

					valset := lastValsetResp.Valsets[0]

					valsets <- valset
				}
			}
		}

	}()

	return valsets, nil
}

type ExtendedDataCommitment struct {
	Commitment bytes.HexBytes
	Start, End int64
	Nonce      uint64
}

func (ac *appClient) SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error) {
	dataCommitments := make(chan ExtendedDataCommitment)

	queryClient := types.NewQueryClient(ac.qgbRPC)

	resp, err := queryClient.Params(ctx, &types.QueryParamsRequest{})
	if err != nil {
		return nil, nil
	}

	params := resp.Params
	window := params.DataCommitmentWindow

	results, err := ac.tendermintRPC.Subscribe(ctx, "height", coretypes.EventQueryNewBlockHeader.String())
	if err != nil {
		return nil, nil
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
				if height%int64(window) != 0 {
					continue
				}

				// TODO: calculate start height some other way that can handle changes
				// in the data window param
				startHeight := height - int64(window)
				endHeight := height

				// create and send the data commitment
				dcResp, err := ac.tendermintRPC.DataCommitment(
					ctx,
					fmt.Sprintf("block.height >= %d AND block.height <= %d",
						startHeight,
						endHeight,
					),
				)
				if err != nil {
					ac.logger.Error(err.Error())
					continue
				}

				// TODO: store the nonce in the state somehwere, so that we don't have
				// to assume that the nonce on the evm chain is up to date!!!
				nonce, err := ac.getNonce()
				if err != nil {
					ac.logger.Error(err.Error())
					continue
				}

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

func (ac *appClient) getNonce() (uint64, error) {
	// todo implement after we commit the nonce to state.
	return 0, nil
}
