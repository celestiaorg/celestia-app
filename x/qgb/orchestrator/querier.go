package orchestrator

import (
	"context"
	"fmt"
	"time"

	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/tendermint/spm/cosmoscmd"
	"github.com/tendermint/tendermint/libs/bytes"
	coretypes "github.com/tendermint/tendermint/rpc/core/types"

	"github.com/tendermint/tendermint/rpc/client/http"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"google.golang.org/grpc"
)

var _ Querier = &querier{}

type Querier interface {
	// attestation queries
	QueryAttestationByNonce(ctx context.Context, nonce uint64) (types.AttestationRequestI, error)
	QueryLatestAttestationNonce(ctx context.Context) (uint64, error)

	// data commitment queries
	QueryDataCommitmentByNonce(ctx context.Context, nonce uint64) (*types.DataCommitment, error)

	// data commitment confirm queries
	QueryDataCommitmentConfirms(ctx context.Context, commit string) ([]types.MsgDataCommitmentConfirm, error)
	QueryDataCommitmentConfirm(
		ctx context.Context,
		endBlock uint64,
		beginBlock uint64,
		address string,
	) (*types.MsgDataCommitmentConfirm, error)
	QueryDataCommitmentConfirmsByExactRange(
		ctx context.Context,
		start uint64,
		end uint64,
	) ([]types.MsgDataCommitmentConfirm, error)
	QueryTwoThirdsDataCommitmentConfirms(
		ctx context.Context,
		timeout time.Duration,
		dc types.DataCommitment,
	) ([]types.MsgDataCommitmentConfirm, error)

	// valset queries
	QueryValsetByNonce(ctx context.Context, nonce uint64) (*types.Valset, error)
	QueryLatestValset(ctx context.Context) (*types.Valset, error)
	QueryLastValsetBeforeNonce(
		ctx context.Context,
		nonce uint64,
	) (*types.Valset, error)

	// valset confirm queries
	QueryTwoThirdsValsetConfirms(
		ctx context.Context,
		timeout time.Duration,
		valset types.Valset,
	) ([]types.MsgValsetConfirm, error)
	QueryValsetConfirm(ctx context.Context, nonce uint64, address string) (*types.MsgValsetConfirm, error)

	// misc queries
	QueryHeight(ctx context.Context) (uint64, error)
	QueryLastUnbondingHeight(ctx context.Context) (uint64, error)

	// tendermint
	QueryCommitment(ctx context.Context, query string) (bytes.HexBytes, error)
	SubscribeEvents(ctx context.Context, subscriptionName string, eventName string) (<-chan coretypes.ResultEvent, error)
}

type querier struct {
	qgbRPC        *grpc.ClientConn
	logger        tmlog.Logger
	tendermintRPC *http.HTTP
	encCfg        cosmoscmd.EncodingConfig
}

func NewQuerier(
	qgbRPCAddr, tendermintRPC string,
	logger tmlog.Logger,
	encCft cosmoscmd.EncodingConfig,
) (*querier, error) {
	qgbGRPC, err := grpc.Dial(qgbRPCAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	trpc, err := http.New(tendermintRPC, "/websocket")
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
		encCfg:        encCft,
	}, nil
}

// TODO add the other stop methods for other clients.
func (q *querier) Stop() {
	err := q.qgbRPC.Close()
	if err != nil {
		q.logger.Error(err.Error())
	}
	err = q.tendermintRPC.Stop()
	if err != nil {
		q.logger.Error(err.Error())
	}
}

func (q *querier) QueryDataCommitmentConfirms(
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
	dc types.DataCommitment,
) ([]types.MsgDataCommitmentConfirm, error) {
	valset, err := q.QueryLastValsetBeforeNonce(ctx, dc.Nonce)
	if err != nil {
		return nil, err
	}

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
			return nil, errors.Wrap(
				ErrNotEnoughDataCommitmentConfirms,
				fmt.Sprintf("failure to query for majority validator set confirms: timout %s", timeout),
			)
		default:
			currThreshHold := uint64(0)
			confirms, err := q.QueryDataCommitmentConfirmsByExactRange(ctx, dc.BeginBlock, dc.EndBlock)
			if err != nil {
				return nil, err
			}

			correctConfirms := make([]types.MsgDataCommitmentConfirm, 0)
			for _, dataCommitmentConfirm := range confirms {
				val, has := vals[dataCommitmentConfirm.EthAddress]
				if !has {
					// currently, the orchestrators sign everything even if they didn't exist during a certain valset
					// thus, the Relayer finds correct confirms and also incorrect ones. By incorrect, I mean signatures from
					// orchestrators that didn't belong to the valset in question, but they still signed it
					// as part of their catching up mechanism.
					// should be fixed with the new design and https://github.com/celestiaorg/celestia-app/issues/406
					q.logger.Debug(fmt.Sprintf(
						"dataCommitmentConfirm signer not found in stored validator set: address %s nonce %d",
						val.EthereumAddress,
						valset.Nonce,
					))
					continue
				}
				currThreshHold += val.Power
				correctConfirms = append(correctConfirms, dataCommitmentConfirm)
			}

			if currThreshHold >= majThreshHold {
				return correctConfirms, nil
			}
			q.logger.Debug(
				fmt.Sprintf(
					"found DataCommitmentConfirms total power %d number of confirms %d",
					currThreshHold,
					len(confirms),
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
	var currentValset types.Valset
	if valset.Nonce == 1 {
		currentValset = valset
	} else {
		vs, err := q.QueryLastValsetBeforeNonce(ctx, valset.Nonce)
		if err != nil {
			return nil, err
		}
		currentValset = *vs
	}
	// create a map to easily search for power
	vals := make(map[string]types.BridgeValidator)
	for _, val := range currentValset.Members {
		vals[val.GetEthereumAddress()] = val
	}

	majThreshHold := valset.TwoThirdsThreshold()

	for {
		select {
		case <-ctx.Done():
			return nil, nil
		// TODO: remove this extra case, and we can instead rely on the caller to pass a context with a timeout
		case <-time.After(timeout):
			return nil, errors.Wrap(
				ErrNotEnoughValsetConfirms,
				fmt.Sprintf("failure to query for majority validator set confirms: timout %s", timeout),
			)
		default:
			currThreshHold := uint64(0)
			queryClient := types.NewQueryClient(q.qgbRPC)
			confirmsResp, err := queryClient.ValsetConfirmsByNonce(ctx, &types.QueryValsetConfirmsByNonceRequest{
				Nonce: valset.Nonce,
			})
			if err != nil {
				return nil, err
			}

			confirms := make([]types.MsgValsetConfirm, 0)
			for _, valsetConfirm := range confirmsResp.Confirms {
				val, has := vals[valsetConfirm.EthAddress]
				if !has {
					// currently, the orchestrators sign everything even if they didn't exist during a certain valset
					// thus, the Relayer finds correct confirms and also incorrect ones. By incorrect, I mean signatures from
					// orchestrators that didn't belong to the valset in question, but they still signed it
					// as part of their catching up mechanism.
					// should be fixed with the new design. and https://github.com/celestiaorg/celestia-app/issues/406
					q.logger.Debug(
						fmt.Sprintf(
							"valSetConfirm signer not found in stored validator set: address %s nonce %d",
							val.EthereumAddress,
							valset.Nonce,
						))
					continue
				}
				currThreshHold += val.Power
				confirms = append(confirms, valsetConfirm)
			}

			if currThreshHold >= majThreshHold {
				return confirms, nil
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

// QueryLastValsetBeforeNonce returns the last valset before nonce.
// the provided `nonce` can be a valset, but this will return the valset before it.
// If nonce is 1, it will return an error. Because, there is no valset before nonce 1.
func (q *querier) QueryLastValsetBeforeNonce(ctx context.Context, nonce uint64) (*types.Valset, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)
	resp, err := queryClient.LastValsetRequestBeforeNonce(
		ctx,
		&types.QueryLastValsetRequestBeforeNonceRequest{Nonce: nonce},
	)
	if err != nil {
		return nil, err
	}

	return resp.Valset, nil
}

func (q *querier) QueryValsetConfirm(
	ctx context.Context,
	nonce uint64,
	address string,
) (*types.MsgValsetConfirm, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)
	// FIXME this is not always a valset confirm (the nonce can be of a data commitment)
	// and might return an empty list. Should we worry?
	resp, err := queryClient.ValsetConfirm(ctx, &types.QueryValsetConfirmRequest{Nonce: nonce, Address: address})
	if err != nil {
		return nil, err
	}

	return resp.Confirm, nil
}

func (q *querier) QueryHeight(ctx context.Context) (uint64, error) {
	resp, err := q.tendermintRPC.Status(ctx)
	if err != nil {
		return 0, err
	}

	return uint64(resp.SyncInfo.LatestBlockHeight), nil
}

func (q *querier) QueryLastUnbondingHeight(ctx context.Context) (uint64, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)
	resp, err := queryClient.LastUnbondingHeight(ctx, &types.QueryLastUnbondingHeightRequest{})
	if err != nil {
		return 0, err
	}

	return resp.Height, nil
}

func (q *querier) QueryDataCommitmentConfirm(
	ctx context.Context,
	endBlock uint64,
	beginBlock uint64,
	address string,
) (*types.MsgDataCommitmentConfirm, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)

	confirmsResp, err := queryClient.DataCommitmentConfirm(
		ctx,
		&types.QueryDataCommitmentConfirmRequest{
			EndBlock:   endBlock,
			BeginBlock: beginBlock,
			Address:    address,
		},
	)
	if err != nil {
		return nil, err
	}

	return confirmsResp.Confirm, nil
}

func (q *querier) QueryDataCommitmentConfirmsByExactRange(
	ctx context.Context,
	start uint64,
	end uint64,
) ([]types.MsgDataCommitmentConfirm, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)
	confirmsResp, err := queryClient.DataCommitmentConfirmsByExactRange(
		ctx,
		&types.QueryDataCommitmentConfirmsByExactRangeRequest{
			BeginBlock: start,
			EndBlock:   end,
		},
	)
	if err != nil {
		return nil, err
	}
	return confirmsResp.Confirms, nil
}

func (q *querier) QueryDataCommitmentByNonce(ctx context.Context, nonce uint64) (*types.DataCommitment, error) {
	attestation, err := q.QueryAttestationByNonce(ctx, nonce)
	if err != nil {
		return nil, err
	}

	if attestation.Type() != types.DataCommitmentRequestType {
		return nil, types.ErrAttestationNotDataCommitmentRequest
	}

	dcc, ok := attestation.(*types.DataCommitment)
	if !ok {
		return nil, types.ErrAttestationNotDataCommitmentRequest
	}

	return dcc, nil
}

func (q *querier) QueryAttestationByNonce(
	ctx context.Context,
	nonce uint64,
) (types.AttestationRequestI, error) { // FIXME is it alright to return interface?
	queryClient := types.NewQueryClient(q.qgbRPC)
	atResp, err := queryClient.AttestationRequestByNonce(
		ctx,
		&types.QueryAttestationRequestByNonceRequest{Nonce: nonce},
	)
	if err != nil {
		return nil, err
	}
	if atResp.Attestation == nil {
		return nil, types.ErrAttestationNotFound
	}

	unmarshalledAttestation, err := q.unmarshallAttestation(atResp.Attestation)
	if err != nil {
		return nil, err
	}

	return unmarshalledAttestation, nil
}

func (q *querier) QueryValsetByNonce(ctx context.Context, nonce uint64) (*types.Valset, error) {
	attestation, err := q.QueryAttestationByNonce(ctx, nonce)
	if err != nil {
		return nil, err
	}

	if attestation.Type() != types.ValsetRequestType {
		return nil, types.ErrAttestationNotValsetRequest
	}

	value, ok := attestation.(*types.Valset)
	if !ok {
		return nil, ErrUnmarshallValset
	}

	return value, nil
}

func (q *querier) QueryLatestValset(ctx context.Context) (*types.Valset, error) {
	latestNonce, err := q.QueryLatestAttestationNonce(ctx)
	if err != nil {
		return nil, err
	}

	var latestValset *types.Valset
	if vs, err := q.QueryValsetByNonce(ctx, latestNonce); err == nil {
		latestValset = vs
	} else {
		latestValset, err = q.QueryLastValsetBeforeNonce(ctx, latestNonce)
		if err != nil {
			return nil, err
		}
	}
	return latestValset, nil
}

func (q *querier) QueryLatestAttestationNonce(ctx context.Context) (uint64, error) {
	queryClient := types.NewQueryClient(q.qgbRPC)

	resp, err := queryClient.LatestAttestationNonce(
		ctx,
		&types.QueryLatestAttestationNonceRequest{},
	)
	if err != nil {
		return 0, err
	}

	return resp.Nonce, nil
}

// QueryCommitment queries the commitment over a set of blocks defined in the query.
func (q querier) QueryCommitment(ctx context.Context, query string) (bytes.HexBytes, error) {
	dcResp, err := q.tendermintRPC.DataCommitment(ctx, query)
	if err != nil {
		return nil, err
	}
	return dcResp.DataCommitment, nil
}

func (q querier) SubscribeEvents(ctx context.Context, subscriptionName string, eventName string) (<-chan coretypes.ResultEvent, error) {
	// This doesn't seem to complain when the node is down
	results, err := q.tendermintRPC.Subscribe(
		ctx,
		"attestation-changes",
		fmt.Sprintf("%s.%s='%s'", types.EventTypeAttestationRequest, sdk.AttributeKeyModule, types.ModuleName),
	)
	if err != nil {
		return nil, err
	}
	return results, err
}

func (q *querier) unmarshallAttestation(attestation *cdctypes.Any) (types.AttestationRequestI, error) {
	var unmarshalledAttestation types.AttestationRequestI
	err := q.encCfg.InterfaceRegistry.UnpackAny(attestation, &unmarshalledAttestation)
	if err != nil {
		return nil, err
	}
	return unmarshalledAttestation, nil
}
