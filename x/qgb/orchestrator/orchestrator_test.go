package orchestrator

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ AppClient = &mockAppClient{}

type mockAppClient struct {
	valsets     chan types.Valset
	commitments chan ExtendedDataCommitment
}

func newMockAppClient() *mockAppClient {
	return &mockAppClient{
		valsets:     make(chan types.Valset, 10),
		commitments: make(chan ExtendedDataCommitment, 10),
	}
}

func (mac *mockAppClient) pushValidatorSet(valset types.Valset) {
	mac.valsets <- valset
}

func (mac *mockAppClient) pushDataCommitment(commit ExtendedDataCommitment) {
	mac.commitments <- commit
}

func (mac *mockAppClient) SubscribeValset(ctx context.Context) (<-chan types.Valset, error) {
	return nil, nil
}

func (mac *mockAppClient) SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error) {
	return nil, nil
}

func (mac *mockAppClient) BroadcastTx(ctx context.Context, msg sdk.Msg) error {
	return nil
}

func (mac *mockAppClient) QueryDataCommitments(ctx context.Context, commit string) ([]types.MsgDataCommitmentConfirm, error) {
	return nil, nil
}
