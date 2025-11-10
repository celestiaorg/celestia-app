package fibre_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/fibre"
	grpcfibre "github.com/celestiaorg/celestia-app/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// testEnv holds the test environment with servers, clients, and validator set
type testEnv struct {
	valSet      validator.Set
	grpcServers []*grpc.Server
	clients     []*fibre.Client
	stores      []*fibre.Store
}

func (e *testEnv) Close() {
	for _, srv := range e.grpcServers {
		srv.Stop()
	}
	for _, client := range e.clients {
		_ = client.Close()
	}
	for _, store := range e.stores {
		if store != nil {
			_ = store.Close()
		}
	}
}

// ForEachClient runs the given function for each client concurrently and waits for completion.
// Returns the first error encountered, if any.
func (e *testEnv) ForEachClient(ctx context.Context, fn func(context.Context, *fibre.Client, int) error) error {
	g, ctx := errgroup.WithContext(ctx)

	for i, client := range e.clients {
		g.Go(func() error {
			return fn(ctx, client, i)
		})
	}

	return g.Wait()
}

// ForEachStore runs the given function for each store concurrently and waits for completion.
// Returns the first error encountered, if any.
func (e *testEnv) ForEachStore(ctx context.Context, fn func(context.Context, *fibre.Store, int) error) error {
	g, ctx := errgroup.WithContext(ctx)

	for i, store := range e.stores {
		g.Go(func() error {
			return fn(ctx, store, i)
		})
	}

	return g.Wait()
}

// makeTestEnv creates a complete test environment with validators, servers, and clients
func makeTestEnv(
	t *testing.T,
	numValidators int,
	numClients int,
	modifyClientConfig func(*fibre.ClientConfig),
	modifyServerConfig func(*fibre.ServerConfig),
) *testEnv {
	t.Helper()

	validators, privKeys := makeTestValidators(t, numValidators)
	valSet := validator.Set{
		ValidatorSet: core.NewValidatorSet(validators),
		Height:       100,
	}

	grpcServers, stores, addresses := makeTestServers(t, validators, privKeys, valSet, modifyServerConfig)
	clients := make([]*fibre.Client, numClients)
	for i := range numClients {
		clientCfg := fibre.DefaultClientConfig()
		clientCfg.NewClientFn = grpcfibre.DefaultNewClientFn(&testHostRegistry{addresses: addresses})

		// create logger with unique client identifier
		clientCfg.Log = slog.Default().With("client_idx", i)

		// apply optional client config modifications
		if modifyClientConfig != nil {
			modifyClientConfig(&clientCfg)
		}

		client, err := fibre.NewClient(nil, makeTestKeyring(t), &mockValidatorSetGetter{set: valSet}, &mockHostRegistry{}, clientCfg)
		require.NoError(t, err)
		clients[i] = client
	}

	return &testEnv{
		valSet:      valSet,
		grpcServers: grpcServers,
		clients:     clients,
		stores:      stores,
	}
}

// makeTestServers creates and starts gRPC servers for each validator
func makeTestServers(
	t *testing.T,
	validators []*core.Validator,
	privKeys []cmted25519.PrivKey,
	valSet validator.Set,
	modifyServerConfig func(*fibre.ServerConfig),
) ([]*grpc.Server, []*fibre.Store, map[string]string) {
	t.Helper()

	grpcServers := make([]*grpc.Server, len(validators))
	fibreServers := make([]*fibre.Server, len(validators))
	stores := make([]*fibre.Store, len(validators))
	addresses := make(map[string]string)

	for i, val := range validators {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		serverCfg := fibre.DefaultServerConfig()
		// Set a temporary directory for the BadgerDB store
		tmpDir := t.TempDir()
		serverCfg.Path = filepath.Join(tmpDir, "fibre-store")
		// create logger with unique server identifier
		serverCfg.Log = slog.Default().With(
			"server_idx", i,
			"server_addr", listener.Addr().String(),
			"validator_addr", val.Address.String(),
		)

		// apply optional server config modifications
		if modifyServerConfig != nil {
			modifyServerConfig(&serverCfg)
		}

		// Create gRPC server with mock services
		grpcServer := grpc.NewServer()

		// Register mock Query service
		mockQueryServer := &mockQueryServer{}
		types.RegisterQueryServer(grpcServer, mockQueryServer)

		// Register mock BlockAPI service
		valSetProto, err := valSet.ToProto()
		require.NoError(t, err)
		mockBlockAPIServer := &mockBlockAPIServer{
			validatorSetResponse: &coregrpc.ValidatorSetResponse{
				ValidatorSet: valSetProto,
				Height:       int64(valSet.Height),
			},
		}
		coregrpc.RegisterBlockAPIServer(grpcServer, mockBlockAPIServer)

		// Create client connection to the mock server (will be used after server starts)
		// Create connection before starting server
		conn, err := grpc.NewClient(
			listener.Addr().String(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		require.NoError(t, err)

		// Create Fibre server - this will register the Fibre service on grpcServer
		fibreServer, err := fibre.NewServerFromGRPC(
			newTestPrivValidator(privKeys[i]),
			grpcServer,
			conn,
			serverCfg,
		)
		require.NoError(t, err)

		// Now start the gRPC server after all services are registered
		go func() { _ = grpcServer.Serve(listener) }()

		grpcServers[i] = grpcServer
		fibreServers[i] = fibreServer
		// Extract store from server for test access
		stores[i] = fibreServer.Store()
		addresses[val.Address.String()] = listener.Addr().String()
	}

	return grpcServers, stores, addresses
}

// testHostRegistry implements validator.HostRegistry for testing
type testHostRegistry struct {
	addresses map[string]string
}

func (r *testHostRegistry) GetHost(ctx context.Context, val *core.Validator) (validator.Host, error) {
	addr, ok := r.addresses[val.Address.String()]
	if !ok {
		return "", fmt.Errorf("no address for validator %s", val.Address.String())
	}
	return validator.Host(addr), nil
}

// mockQueryServer is a mock implementation of types.QueryServer for testing.
type mockQueryServer struct {
	types.UnimplementedQueryServer
}

func (m *mockQueryServer) Params(ctx context.Context, in *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	return &types.QueryParamsResponse{}, nil
}

func (m *mockQueryServer) EscrowAccount(ctx context.Context, in *types.QueryEscrowAccountRequest) (*types.QueryEscrowAccountResponse, error) {
	return &types.QueryEscrowAccountResponse{}, nil
}

func (m *mockQueryServer) Withdrawals(ctx context.Context, in *types.QueryWithdrawalsRequest) (*types.QueryWithdrawalsResponse, error) {
	return &types.QueryWithdrawalsResponse{}, nil
}

func (m *mockQueryServer) IsPaymentProcessed(ctx context.Context, in *types.QueryIsPaymentProcessedRequest) (*types.QueryIsPaymentProcessedResponse, error) {
	return &types.QueryIsPaymentProcessedResponse{}, nil
}

func (m *mockQueryServer) ValidatePaymentPromise(ctx context.Context, in *types.QueryValidatePaymentPromiseRequest) (*types.QueryValidatePaymentPromiseResponse, error) {
	// Always return valid for testing
	return &types.QueryValidatePaymentPromiseResponse{IsValid: true}, nil
}

// mockBlockAPIServer is a mock implementation of coregrpc.BlockAPIServer for testing.
type mockBlockAPIServer struct {
	coregrpc.UnimplementedBlockAPIServer
	validatorSetResponse *coregrpc.ValidatorSetResponse
}

func (m *mockBlockAPIServer) ValidatorSet(ctx context.Context, req *coregrpc.ValidatorSetRequest) (*coregrpc.ValidatorSetResponse, error) {
	return m.validatorSetResponse, nil
}
