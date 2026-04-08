package grpc_test

import (
	"context"
	"testing"

	fibregrpc "github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestSetGetter_GetByHeight_Success(t *testing.T) {
	const testHeight = uint64(100)

	// Create test validators
	validators := makeTestValidators(t, 3)
	valSet := core.NewValidatorSet(validators)

	// Convert to proto for the response
	valSetProto, err := valSet.ToProto()
	require.NoError(t, err)

	// Create mock client that returns the validator set
	mockClient := &mockValidatorSetClient{
		response: &coregrpc.ValidatorSetResponse{
			ValidatorSet: valSetProto,
			Height:       int64(testHeight),
		},
	}

	// Create getter and fetch validator set
	getter := fibregrpc.NewSetGetter(mockClient)
	result, err := getter.GetByHeight(context.Background(), testHeight)

	// Verify results
	require.NoError(t, err)
	require.Equal(t, testHeight, result.Height)
	require.NotNil(t, result.ValidatorSet)
	require.Len(t, result.Validators, 3)
	require.Equal(t, int64(300), result.TotalVotingPower()) // 3 validators * 100 voting power each
}

func TestSetGetter_GetByHeight_ZeroHeight(t *testing.T) {
	// Test that GetByHeight rejects zero height
	var client coregrpc.BlockAPIClient
	getter := fibregrpc.NewSetGetter(client)

	_, err := getter.GetByHeight(context.Background(), 0)
	if err == nil {
		t.Error("GetByHeight should reject zero height")
	}
	if err.Error() != "height must be greater than 0, use Head() to get the latest validator set" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

// makeTestValidators creates n validators for testing.
func makeTestValidators(t *testing.T, n int) []*core.Validator {
	t.Helper()
	validators := make([]*core.Validator, n)
	for i := range n {
		privKey := cmted25519.GenPrivKey()
		validators[i] = &core.Validator{
			Address:     privKey.PubKey().Address(),
			PubKey:      privKey.PubKey(),
			VotingPower: 100,
		}
	}
	return validators
}

// mockValidatorSetClient mocks the ValidatorSet method for testing.
type mockValidatorSetClient struct {
	MockBlockAPIClient
	response *coregrpc.ValidatorSetResponse
}

func (m *mockValidatorSetClient) ValidatorSet(ctx context.Context, req *coregrpc.ValidatorSetRequest, opts ...grpc.CallOption) (*coregrpc.ValidatorSetResponse, error) {
	return m.response, nil
}

// MockBlockAPIClient is a simple mock for testing interface compliance
type MockBlockAPIClient struct {
	coregrpc.UnimplementedBlockAPIServer
}

func (m *MockBlockAPIClient) Status(ctx context.Context, req *coregrpc.StatusRequest, opts ...grpc.CallOption) (*coregrpc.StatusResponse, error) {
	return &coregrpc.StatusResponse{}, nil
}

func (m *MockBlockAPIClient) Commit(ctx context.Context, req *coregrpc.CommitRequest, opts ...grpc.CallOption) (*coregrpc.CommitResponse, error) {
	return &coregrpc.CommitResponse{}, nil
}

func (m *MockBlockAPIClient) ValidatorSet(ctx context.Context, req *coregrpc.ValidatorSetRequest, opts ...grpc.CallOption) (*coregrpc.ValidatorSetResponse, error) {
	return &coregrpc.ValidatorSetResponse{}, nil
}

func (m *MockBlockAPIClient) BlockByHeight(ctx context.Context, req *coregrpc.BlockByHeightRequest, opts ...grpc.CallOption) (coregrpc.BlockAPI_BlockByHeightClient, error) {
	return nil, nil // For now, just return nil to test interface compliance
}

func (m *MockBlockAPIClient) BlockByHash(ctx context.Context, req *coregrpc.BlockByHashRequest, opts ...grpc.CallOption) (coregrpc.BlockAPI_BlockByHashClient, error) {
	return nil, nil
}

func (m *MockBlockAPIClient) SubscribeNewHeights(ctx context.Context, req *coregrpc.SubscribeNewHeightsRequest, opts ...grpc.CallOption) (coregrpc.BlockAPI_SubscribeNewHeightsClient, error) {
	return nil, nil
}
