package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	valtypes "github.com/celestiaorg/celestia-app/v10/x/valaddr/types"
	corep2p "github.com/cometbft/cometbft/proto/tendermint/p2p"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	core "github.com/cometbft/cometbft/types"
	tmservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestRunPreencodeUploads(t *testing.T) {
	const duration = 5 * time.Second
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	dir := t.TempDir()
	kr, err := keyring.New(app.Name, keyring.BackendTest, dir, nil, encCfg.Codec)
	require.NoError(t, err)
	signers := make(map[string]int)
	for i := range 2 {
		record, _, err := kr.NewMnemonic(fmt.Sprintf("fibre-%d", i), keyring.English, sdk.FullFundraiserPath, "", hd.Secp256k1)
		require.NoError(t, err)
		pubKey, err := record.GetPubKey()
		require.NoError(t, err)
		signers[string(pubKey.Bytes())] = 0
	}
	pv := core.NewMockPV()
	pubKey, err := pv.GetPubKey()
	require.NoError(t, err)
	valSet, err := core.NewValidatorSet([]*core.Validator{core.NewValidator(pubKey, 1)}).ToProto()
	require.NoError(t, err)

	var mu sync.Mutex
	var promises []*fibretypes.PaymentPromise
	var host string
	var setupDelay sync.Once
	chain := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		switch req := req.(type) {
		case *tmservice.GetNodeInfoRequest:
			return &tmservice.GetNodeInfoResponse{DefaultNodeInfo: &corep2p.DefaultNodeInfo{Network: "preencode-test"}}, nil
		case *valtypes.QueryAllBondedFibreProvidersRequest:
			mu.Lock()
			defer mu.Unlock()
			return &valtypes.QueryAllBondedFibreProvidersResponse{Providers: []valtypes.FibreProvider{{
				ValidatorConsensusAddress: sdk.ConsAddress(pubKey.Address()).String(), Info: valtypes.FibreProviderInfo{Host: host},
			}}}, nil
		case *coregrpc.ValidatorSetRequest:
			return &coregrpc.ValidatorSetResponse{ValidatorSet: valSet, Height: 1}, nil
		case *authtypes.QueryAccountRequest:
			// Setup must not consume the load window, even when it takes longer than the run.
			setupDelay.Do(func() { time.Sleep(2 * duration) })
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			account, err := codectypes.NewAnyWithValue(&authtypes.BaseAccount{Address: req.Address})
			return &authtypes.QueryAccountResponse{Account: account}, err
		case *fibretypes.QueryValidatePaymentPromiseRequest:
			mu.Lock()
			promises = append(promises, &req.Promise)
			mu.Unlock()
			expires := time.Now().Add(time.Hour)
			return &fibretypes.QueryValidatePaymentPromiseResponse{IsValid: true, ExpirationTime: &expires, ShardRetention: time.Hour}, nil
		default:
			return nil, fmt.Errorf("unexpected chain query: %T", req)
		}
	}))
	tmservice.RegisterServiceServer(chain, &tmservice.UnimplementedServiceServer{})
	valtypes.RegisterQueryServer(chain, &valtypes.UnimplementedQueryServer{})
	coregrpc.RegisterBlockAPIServer(chain, &coregrpc.UnimplementedBlockAPIServer{})
	authtypes.RegisterQueryServer(chain, &authtypes.UnimplementedQueryServer{})
	fibretypes.RegisterQueryServer(chain, &fibretypes.UnimplementedQueryServer{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = chain.Serve(listener) }()
	t.Cleanup(chain.Stop)

	serverCfg := fibre.DefaultServerConfig()
	serverCfg.AppGRPCAddress = listener.Addr().String()
	serverCfg.ServerListenAddress = "127.0.0.1:0"
	serverCfg.UnlimitedBudget = true
	serverCfg.SignerFn = func(string) (core.PrivValidator, error) { return pv, nil }
	store := fibre.NewMemoryStore(serverCfg.StoreConfig)
	serverCfg.StoreFn = func(fibre.StoreConfig) (*fibre.Store, error) { return store, nil }
	server, err := fibre.NewServer(serverCfg)
	require.NoError(t, err)
	require.NoError(t, server.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, server.Stop(context.Background())) })
	mu.Lock()
	host = server.ListenAddress()
	mu.Unlock()

	require.NoError(t, run(config{
		grpcEndpoint: listener.Addr().String(), keyringDir: dir, keyPrefix: "fibre",
		concurrency: 2, blobSize: 1024, preencode: true, uploadOnly: true,
		duration: duration, interval: 100 * time.Millisecond,
	}))
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, promises)
	namespaces := make(map[string]bool)
	for _, pb := range promises {
		var promise fibre.PaymentPromise
		require.NoError(t, promise.FromProto(pb))
		require.NoError(t, promise.Validate())
		require.Equal(t, promises[0].Commitment, pb.Commitment, "all workers must reuse the prepared payload")
		require.False(t, namespaces[string(pb.Namespace)], "each upload must have a fresh namespace")
		namespaces[string(pb.Namespace)] = true
		signer := string(pb.SignerPublicKey.Bytes())
		require.Contains(t, signers, signer)
		hash, err := promise.Hash()
		require.NoError(t, err)
		stored, err := store.Has(t.Context(), promise.Commitment, hash)
		require.NoError(t, err)
		if stored {
			signers[signer]++
		}
	}
	for signer, count := range signers {
		require.GreaterOrEqual(t, count, 2, "worker %x must complete repeated uploads after slow setup", signer)
	}
}
