package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"github.com/tendermint/tendermint/rpc/client/http"
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"google.golang.org/grpc"
)

var _ Querier = &querier{}

type Querier interface {
	QueryDataCommitments(ctx context.Context, commit string) ([]types.MsgDataCommitmentConfirm, error)
	QueryLastValset(ctx context.Context) (types.Valset, error)
	QueryTwoThirdsDataCommitmentConfirms(
		ctx context.Context,
		timeout time.Duration,
		commitment string,
	) ([]types.MsgDataCommitmentConfirm, error)
	QueryTwoThirdsValsetConfirms(
		ctx context.Context,
		timeout time.Duration,
		valset types.Valset,
	) ([]types.MsgValsetConfirm, error)
	QueryLastValsets(ctx context.Context) ([]types.Valset, error)
	QueryValsetConfirm(ctx context.Context, nonce uint64, address string) (*types.MsgValsetConfirm, error)
	QueryValsetByNonce(ctx context.Context, nonce uint64) (*types.Valset, error)
	QueryHeight(ctx context.Context) (int64, error)
}

type querier struct {
	qgbRPC        *grpc.ClientConn
	logger        tmlog.Logger
	tendermintRPC *http.HTTP
}

func NewQuerier(qgbRpcAddr, tendermintRpc string, logger tmlog.Logger) (*querier, error) {
	qgbGRPC, err := grpc.Dial(qgbRpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	trpc, err := http.New(tendermintRpc, "/websocket")
	if err != nil {
		return nil, err
	}
	err = trpc.Start()
	if err != nil {
		return nil, err
	}

	return &querier{
		qgbRPC:        qgbGRPC,
		logger:        logger,
		tendermintRPC: trpc,
	}, nil
}

func (q *querier) QueryDataCommitments(
	ctx context.Context,
	commit string,
) ([]types.MsgDataCommitmentConfirm, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)

	confirmsResp, err := queryClient.DataCommitmentConfirmsByCommitment(
		ctx,
		&types.QueryDataCommitmentConfirmsByCommitmentRequest{
			Commitment: commit,
		},
	)
	if err != nil {
		return nil, err
	}

	return confirmsResp.Confirms, nil
}

func (q *querier) QueryTwoThirdsDataCommitmentConfirms(
	ctx context.Context,
	timeout time.Duration,
	commitment string,
) ([]types.MsgDataCommitmentConfirm, error) {
	// query for the latest valset (sorted for us already)
	queryClient := types.NewQueryClient(q.qgbRPC)
	lastValsetResp, err := queryClient.LastValsetRequests(ctx, &types.QueryLastValsetRequestsRequest{})
	if err != nil {
		return nil, err
	}

	if len(lastValsetResp.Valsets) < 1 {
		return nil, errors.New("no validator sets found")
	}

	valset := lastValsetResp.Valsets[0]

	// create a map to easily search for power
	vals := make(map[string]types.BridgeValidator)
	for _, val := range valset.Members {
		vals[val.GetEthereumAddress()] = val
	}

	majThreshHold := valset.TwoThirdsThreshold()

	for {
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(timeout):
			return nil, fmt.Errorf("failure to query for majority validator set confirms: timout %s", timeout)
		default:
			currThreshHold := uint64(0)
			confirmsResp, err := queryClient.DataCommitmentConfirmsByCommitment(
				ctx,
				&types.QueryDataCommitmentConfirmsByCommitmentRequest{
					Commitment: commitment,
				},
			)
			if err != nil {
				return nil, err
			}

			for _, dataCommitmentConfirm := range confirmsResp.Confirms {
				val, has := vals[dataCommitmentConfirm.EthAddress]
				if !has {
					return nil, fmt.Errorf(
						"dataCommitmentConfirm signer not found in stored validator set: address %s nonce %d",
						val.EthereumAddress,
						valset.Nonce,
					)
				}
				currThreshHold += val.Power
			}

			if currThreshHold >= majThreshHold {
				return confirmsResp.Confirms, nil
			}
			q.logger.Debug(
				"foundDataCommitmentConfirms",
				fmt.Sprintf(
					"total power %d number of confirms %d",
					currThreshHold,
					len(confirmsResp.Confirms),
				),
			)
		}
		// TODO: make the timeout configurable
		time.Sleep(time.Second * 30)
	}
}

func (q *querier) QueryTwoThirdsValsetConfirms(
	ctx context.Context,
	timeout time.Duration,
	valset types.Valset,
) ([]types.MsgValsetConfirm, error) {
	// create a map to easily search for power
	vals := make(map[string]types.BridgeValidator)
	for _, val := range valset.Members {
		vals[val.GetEthereumAddress()] = val
	}

	majThreshHold := valset.TwoThirdsThreshold()

	for {
		select {
		case <-ctx.Done():
			return nil, nil
		// TODO: remove this extra case, and we can instead rely on the caller to pass a context with a timeout
		case <-time.After(timeout):
			return nil, fmt.Errorf("failure to query for majority validator set confirms: timout %s", timeout)
		default:
			currThreshHold := uint64(0)
			queryClient := types.NewQueryClient(q.qgbRPC)
			confirmsResp, err := queryClient.ValsetConfirmsByNonce(ctx, &types.QueryValsetConfirmsByNonceRequest{
				Nonce: valset.Nonce,
			})
			if err != nil {
				return nil, err
			}

			for _, valsetConfirm := range confirmsResp.Confirms {
				val, has := vals[valsetConfirm.EthAddress]
				if !has {
					return nil, fmt.Errorf(
						"valSetConfirm signer not found in stored validator set: address %s nonce %d",
						val.EthereumAddress,
						valset.Nonce,
					)
				}
				currThreshHold += val.Power
			}

			if currThreshHold >= majThreshHold {
				return confirmsResp.Confirms, nil
			}
			q.logger.Debug(
				"foundValsetConfirms",
				fmt.Sprintf(
					"total power %d number of confirms %d",
					currThreshHold,
					len(confirmsResp.Confirms),
				),
			)
		}
		// TODO: make the timeout configurable
		time.Sleep(time.Second * 30)
	}
}

// QueryLastValset TODO change name to reflect the functionality correctly
// TODO make this return a pointer
func (q *querier) QueryLastValset(ctx context.Context) (types.Valset, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)
	lastValsetResp, err := queryClient.LastValsetRequests(ctx, &types.QueryLastValsetRequestsRequest{})
	if err != nil {
		return types.Valset{}, err
	}

	if len(lastValsetResp.Valsets) < 2 {
		return types.Valset{}, errors.New("no validator sets found")
	}

	valset := lastValsetResp.Valsets[1]
	return valset, nil
}

func (q *querier) QueryLastValsets(ctx context.Context) ([]types.Valset, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)
	lastValsetResp, err := queryClient.LastValsetRequests(ctx, &types.QueryLastValsetRequestsRequest{})
	if err != nil {
		return nil, err
	}

	return lastValsetResp.Valsets, nil
}

func (q *querier) QueryValsetByNonce(ctx context.Context, nonce uint64) (*types.Valset, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)
	lastValsetResp, err := queryClient.ValsetRequestByNonce(ctx, &types.QueryValsetRequestByNonceRequest{Nonce: nonce})
	if err != nil {
		return nil, err
	}

	return lastValsetResp.Valset, nil
}

func (q *querier) QueryValsetConfirm(ctx context.Context, nonce uint64, address string) (*types.MsgValsetConfirm, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)
	resp, err := queryClient.ValsetConfirm(ctx, &types.QueryValsetConfirmRequest{Nonce: nonce, Address: address})
	if err != nil {
		return nil, err
	}

	return resp.Confirm, nil
}

func (q *querier) QueryHeight(ctx context.Context) (int64, error) {
	resp, err := q.tendermintRPC.Status(ctx)
	if err != nil {
		return 0, err
	}

	return resp.SyncInfo.LatestBlockHeight, nil
}
