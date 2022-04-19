package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/libs/bytes"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/rpc/client/http"
	coretypes "github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"
)

type AppClient interface {
	SubscribeValset(ctx context.Context) (<-chan types.Valset, error)
	SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error)
	BroadcastTx(ctx context.Context, msg sdk.Msg) (string, error)
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
	OrchestratorAddress() sdk.AccAddress
	QueryLastValsets(ctx context.Context) ([]types.Valset, error)
}

type ExtendedDataCommitment struct {
	Commitment bytes.HexBytes
	Start, End int64
	Nonce      uint64
}

type appClient struct {
	tendermintRPC *http.HTTP
	qgbRPC        *grpc.ClientConn
	logger        tmlog.Logger
	// TODO check if we can move the keyring outside from the paytypes.
	signer *paytypes.KeyringSigner
	mutex  *sync.Mutex
}

func NewAppClient(
	logger tmlog.Logger,
	signer *paytypes.KeyringSigner,
	chainID, coreRPC, appRPC string,
) (AppClient, error) {
	trpc, err := http.New(coreRPC, "/websocket")
	if err != nil {
		return nil, err
	}
	err = trpc.Start()
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
		signer:        signer,
		mutex:         &sync.Mutex{},
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

func (ac *appClient) SubscribeValset(ctx context.Context) (<-chan types.Valset, error) {
	valsets := make(chan types.Valset, 10)

	results, err := ac.tendermintRPC.Subscribe(
		ctx,
		"valset-changes",
		fmt.Sprintf("%s.%s='%s'", types.EventTypeValsetRequest, sdk.AttributeKeyModule, types.ModuleName),
	)

	if err != nil {
		return nil, err
	}
	queryClient := types.NewQueryClient(ac.qgbRPC)
	nonces := make([]uint64, 10000)

	go func() {
		defer close(valsets)
		for {
			select {
			case <-ctx.Done():
				return
			case <-results:
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

				// Checking if we already signed this valset
				resp, err := queryClient.ValsetConfirm(
					ctx,
					&types.QueryValsetConfirmRequest{Nonce: valset.Nonce, Address: ac.OrchestratorAddress().String()},
				)
				if err != nil {
					ac.logger.Error(err.Error())
					return
				}
				if resp.Confirm == nil && !contains(nonces, valset.Nonce) {
					nonces = append(nonces, valset.Nonce)
					valsets <- valset
				}
			}
		}
	}()

	return valsets, nil
}

func (ac *appClient) SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error) {
	dataCommitments := make(chan ExtendedDataCommitment)

	// queryClient := types.NewQueryClient(ac.qgbRPC)

	// resp, err := queryClient.Params(ctx, &types.QueryParamsRequest{})
	// if err != nil {
	// 	return nil, err
	// }

	// params := resp.Params
	q := coretypes.EventQueryNewBlockHeader.String()
	results, err := ac.tendermintRPC.Subscribe(ctx, "height", q)
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

func (ac *appClient) BroadcastTx(ctx context.Context, msg sdk.Msg) (string, error) {
	ac.mutex.Lock()
	defer ac.mutex.Unlock()
	err := ac.signer.QueryAccountNumber(ctx, ac.qgbRPC)
	if err != nil {
		return "", err
	}

	builder := ac.signer.NewTxBuilder()
	// TODO make gas limit configurable
	builder.SetGasLimit(9999999999999)
	// TODO: update this api
	// via https://github.com/celestiaorg/celestia-app/pull/187/commits/37f96d9af30011736a3e6048bbb35bad6f5b795c
	tx, err := ac.signer.BuildSignedTx(builder, msg)
	if err != nil {
		return "", err
	}

	rawTx, err := ac.signer.EncodeTx(tx)
	if err != nil {
		return "", err
	}

	resp, err := paytypes.BroadcastTx(ctx, ac.qgbRPC, 1, rawTx)
	if err != nil {
		return "", err
	}

	if resp.TxResponse.Code != 0 {
		return "", fmt.Errorf("failure to broadcast tx: %s", resp.TxResponse.RawLog)
	}

	return resp.TxResponse.TxHash, nil
}

func (ac *appClient) QueryDataCommitments(
	ctx context.Context,
	commit string,
) ([]types.MsgDataCommitmentConfirm, error) {
	queryClient := types.NewQueryClient(ac.qgbRPC)

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

func (ac *appClient) QueryTwoThirdsDataCommitmentConfirms(
	ctx context.Context,
	timeout time.Duration,
	commitment string,
) ([]types.MsgDataCommitmentConfirm, error) {
	// query for the latest valset (sorted for us already)
	queryClient := types.NewQueryClient(ac.qgbRPC)
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
			ac.logger.Debug(
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

func (ac *appClient) QueryTwoThirdsValsetConfirms(
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
			queryClient := types.NewQueryClient(ac.qgbRPC)
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
			ac.logger.Debug(
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

func (ac *appClient) OrchestratorAddress() sdk.AccAddress {
	return ac.signer.GetSignerInfo().GetAddress()
}

// QueryLastValset TODO change name to reflect the functionality correctly
func (ac *appClient) QueryLastValset(ctx context.Context) (types.Valset, error) {
	queryClient := types.NewQueryClient(ac.qgbRPC)
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
func (ac *appClient) QueryLastValsets(ctx context.Context) ([]types.Valset, error) {
	queryClient := types.NewQueryClient(ac.qgbRPC)
	lastValsetResp, err := queryClient.LastValsetRequests(ctx, &types.QueryLastValsetRequestsRequest{})
	if err != nil {
		return nil, err
	}

	return lastValsetResp.Valsets, nil
}
