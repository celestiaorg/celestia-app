package fibre_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/fibre"
	grpcfibre "github.com/celestiaorg/celestia-app/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
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
	testParams := fibre.DefaultProtocolParams
	testParams.MaxValidatorCount = numValidators

	grpcServers, stores, addresses := makeTestServers(t, validators, privKeys, valSet, testParams, modifyServerConfig)
	clients := make([]*fibre.Client, numClients)
	for i := range numClients {
		clientCfg := fibre.NewClientConfigFromParams(testParams)

		// create logger with unique client identifier
		clientCfg.Log = slog.Default().With("client_idx", i)

		// apply optional client config modifications
		if modifyClientConfig != nil {
			modifyClientConfig(&clientCfg)
		}
		clientCfg.NewClientFn = grpcfibre.DefaultNewClientFn(&testHostRegistry{addresses: addresses}, clientCfg.MaxMessageSize)

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
	params fibre.ProtocolParams,
	modifyServerConfig func(*fibre.ServerConfig),
) ([]*grpc.Server, []*fibre.Store, map[string]string) {
	t.Helper()

	grpcServers := make([]*grpc.Server, len(validators))
	stores := make([]*fibre.Store, len(validators))
	addresses := make(map[string]string)

	for i, val := range validators {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		serverCfg := fibre.NewServerConfigFromParams(params)

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

		fibreServer, err := fibre.NewInMemoryServer(
			newTestPrivValidator(privKeys[i]),
			&mockQueryClient{},
			&mockValidatorSetGetter{set: valSet},
			serverCfg,
		)
		require.NoError(t, err)

		maxMsgSize := serverCfg.MaxMessageSize
		grpcServer := grpc.NewServer(
			grpc.MaxRecvMsgSize(maxMsgSize),
			grpc.MaxSendMsgSize(maxMsgSize),
		)
		types.RegisterFibreServer(grpcServer, fibreServer)

		go func() { _ = grpcServer.Serve(listener) }()

		grpcServers[i] = grpcServer
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
