package fibre_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	"github.com/cometbft/cometbft/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// makeTestServer creates a server with all necessary test infrastructure.
func makeTestServer(t *testing.T) (*fibre.Server, validator.Set, *core.Validator) {
	t.Helper()

	// create validator set (use enough validators for good distribution)
	validators, privKeys := makeTestValidators(t, 100)
	valSet := validator.Set{
		ValidatorSet: core.NewValidatorSet(validators),
		Height:       100,
	}

	// use first validator as the server's identity
	privVal := newTestPrivValidator(privKeys[0])

	// find the server validator in the ValidatorSet by matching the address
	// Note: core.NewValidatorSet may reorder validators, so we can't assume validators[0] == privKeys[0]
	serverPubKey, err := privVal.GetPubKey()
	require.NoError(t, err)
	serverAddress := serverPubKey.Address()

	serverValidator, found := valSet.GetByAddress(serverAddress)
	require.True(t, found, "server validator not found in validator set")
	require.NotNil(t, serverValidator, "server validator is nil")

	// create server
	server, err := fibre.NewInMemoryServer(
		privVal,
		&mockQueryClient{},
		&mockValidatorSetGetter{set: valSet},
		fibre.DefaultServerConfig(),
	)
	require.NoError(t, err)

	return server, valSet, serverValidator
}

// mockQueryClient is a mock implementation of types.QueryClient for testing.
type mockQueryClient struct{}

func (m *mockQueryClient) Params(ctx context.Context, in *types.QueryParamsRequest, opts ...grpc.CallOption) (*types.QueryParamsResponse, error) {
	return &types.QueryParamsResponse{}, nil
}

func (m *mockQueryClient) EscrowAccount(ctx context.Context, in *types.QueryEscrowAccountRequest, opts ...grpc.CallOption) (*types.QueryEscrowAccountResponse, error) {
	return &types.QueryEscrowAccountResponse{}, nil
}

func (m *mockQueryClient) Withdrawals(ctx context.Context, in *types.QueryWithdrawalsRequest, opts ...grpc.CallOption) (*types.QueryWithdrawalsResponse, error) {
	return &types.QueryWithdrawalsResponse{}, nil
}

func (m *mockQueryClient) IsPaymentProcessed(ctx context.Context, in *types.QueryIsPaymentProcessedRequest, opts ...grpc.CallOption) (*types.QueryIsPaymentProcessedResponse, error) {
	return &types.QueryIsPaymentProcessedResponse{}, nil
}

func (m *mockQueryClient) ValidatePaymentPromise(ctx context.Context, in *types.QueryValidatePaymentPromiseRequest, opts ...grpc.CallOption) (*types.QueryValidatePaymentPromiseResponse, error) {
	// Calculate expiration time: creation_timestamp + 1 hour (default timeout)
	expirationTime := in.Promise.CreationTimestamp.Add(1 * time.Hour)
	currentTime := time.Now()

	// Check if payment promise has expired
	if currentTime.After(expirationTime) || currentTime.Equal(expirationTime) {
		return nil, fmt.Errorf("payment promise expired: creation_timestamp %v + timeout %v = %v, current_time: %v", in.Promise.CreationTimestamp, 1*time.Hour, expirationTime, currentTime)
	}

	return &types.QueryValidatePaymentPromiseResponse{
		IsValid:        true,
		ExpirationTime: &expirationTime,
	}, nil
}

// testPrivValidator is a simple mock PrivValidator for testing.
type testPrivValidator struct {
	privKey crypto.PrivKey
}

func newTestPrivValidator(privKey crypto.PrivKey) *testPrivValidator {
	return &testPrivValidator{privKey: privKey}
}

func (m *testPrivValidator) GetPubKey() (crypto.PubKey, error) {
	return m.privKey.PubKey(), nil
}

func (m *testPrivValidator) SignRawBytes(chainID, uniqueID string, rawBytes []byte) ([]byte, error) {
	signBytes, err := core.RawBytesMessageSignBytes(chainID, uniqueID, rawBytes)
	if err != nil {
		return nil, err
	}
	return m.privKey.Sign(signBytes)
}

func (m *testPrivValidator) SignVote(chainID string, vote *cmtproto.Vote) error {
	return nil
}

func (m *testPrivValidator) SignProposal(chainID string, proposal *cmtproto.Proposal) error {
	return nil
}

func (m *testPrivValidator) GetAddress() core.Address {
	return m.privKey.PubKey().Address()
}
