package test

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/tendermint/tendermint/libs/bytes"
	coretypes "github.com/tendermint/tendermint/rpc/core/types"
	"time"
)

var _ orchestrator.Querier = &mockQuerier{}

type mockQuerier struct {
	confirms []types.MsgDataCommitmentConfirm
	height   uint64
}

func NewMockQuerier(confirms []types.MsgDataCommitmentConfirm, height uint64) *mockQuerier {
	return &mockQuerier{
		confirms: confirms,
		height:   height,
	}
}

func (q *mockQuerier) Stop() {
}

func (q *mockQuerier) QueryDataCommitmentConfirms(
	ctx context.Context,
	commit string,
) ([]types.MsgDataCommitmentConfirm, error) {
	confirms := make([]types.MsgDataCommitmentConfirm, 0)
	for _, confirm := range q.confirms {
		if confirm.Commitment == commit {
			confirms = append(confirms, confirm)
		}
	}
	return confirms, nil
}

func (q *mockQuerier) QueryTwoThirdsDataCommitmentConfirms(
	ctx context.Context,
	timeout time.Duration,
	dc types.DataCommitment,
) ([]types.MsgDataCommitmentConfirm, error) {
	return nil, nil
}

func (q *mockQuerier) QueryTwoThirdsValsetConfirms(
	ctx context.Context,
	timeout time.Duration,
	valset types.Valset,
) ([]types.MsgValsetConfirm, error) {
	return nil, nil
}

// QueryLastValsetBeforeNonce returns the last valset before nonce.
// the provided `nonce` can be a valset, but this will return the valset before it.
// If nonce is 1, it will return an error. Because, there is no valset before nonce 1.
func (q *mockQuerier) QueryLastValsetBeforeNonce(ctx context.Context, nonce uint64) (*types.Valset, error) {
	return nil, nil
}

func (q *mockQuerier) QueryValsetConfirm(
	ctx context.Context,
	nonce uint64,
	address string,
) (*types.MsgValsetConfirm, error) {
	return nil, nil
}

func (q *mockQuerier) QueryHeight(ctx context.Context) (uint64, error) {
	return q.height, nil
}

func (q *mockQuerier) QueryLastUnbondingHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (q *mockQuerier) QueryDataCommitmentConfirm(
	ctx context.Context,
	endBlock uint64,
	beginBlock uint64,
	address string,
) (*types.MsgDataCommitmentConfirm, error) {
	return nil, nil
}

func (q *mockQuerier) QueryDataCommitmentConfirmsByExactRange(
	ctx context.Context,
	start uint64,
	end uint64,
) ([]types.MsgDataCommitmentConfirm, error) {
	return nil, nil
}

func (q *mockQuerier) QueryDataCommitmentByNonce(ctx context.Context, nonce uint64) (*types.DataCommitment, error) {
	return nil, nil
}

func (q *mockQuerier) QueryAttestationByNonce(
	ctx context.Context,
	nonce uint64,
) (types.AttestationRequestI, error) {
	return nil, nil
}

func (q *mockQuerier) QueryValsetByNonce(ctx context.Context, nonce uint64) (*types.Valset, error) {
	return nil, nil
}

func (q *mockQuerier) QueryLatestValset(ctx context.Context) (*types.Valset, error) {
	return nil, nil
}

func (q *mockQuerier) QueryLatestAttestationNonce(ctx context.Context) (uint64, error) {
	return 0, nil
}

// QueryCommitment queries the commitment over a set of blocks defined in the query.
func (q mockQuerier) QueryCommitment(ctx context.Context, query string) (bytes.HexBytes, error) {
	return commitmentFromQuery(query), nil
}

func (q mockQuerier) SubscribeEvents(ctx context.Context, subscriptionName string, eventName string) (<-chan coretypes.ResultEvent, error) {
	return nil, nil
}
