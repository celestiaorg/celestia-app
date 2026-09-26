package user_test

import (
	"context"
	"net"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	minfeetypes "github.com/celestiaorg/celestia-app/v10/x/minfee/types"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type mockNodeServer struct {
	nodeservice.UnimplementedServiceServer
	localMinGasPrice string
}

func (m *mockNodeServer) Config(context.Context, *nodeservice.ConfigRequest) (*nodeservice.ConfigResponse, error) {
	return &nodeservice.ConfigResponse{MinimumGasPrice: m.localMinGasPrice}, nil
}

type mockMinfeeServer struct {
	minfeetypes.UnimplementedQueryServer
	networkMinGasPrice math.LegacyDec
}

func (m *mockMinfeeServer) NetworkMinGasPrice(context.Context, *minfeetypes.QueryNetworkMinGasPrice) (*minfeetypes.QueryNetworkMinGasPriceResponse, error) {
	return &minfeetypes.QueryNetworkMinGasPriceResponse{NetworkMinGasPrice: m.networkMinGasPrice}, nil
}

// newMinGasPriceConn serves the node config service and, if minfee is non-nil, the minfee query service.
func newMinGasPriceConn(t *testing.T, localMinGasPrice string, minfee *mockMinfeeServer) *grpc.ClientConn {
	grpcCodec := codec.NewProtoCodec(codectypes.NewInterfaceRegistry()).GRPCCodec()
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer(grpc.ForceServerCodec(grpcCodec))
	nodeservice.RegisterServiceServer(s, &mockNodeServer{localMinGasPrice: localMinGasPrice})
	if minfee != nil {
		minfeetypes.RegisterQueryServer(s, minfee)
	}
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcCodec)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestQueryMinimumGasPrice(t *testing.T) {
	testCases := []struct {
		name   string
		minfee *mockMinfeeServer
		want   float64
	}{
		{
			name:   "network price above local price",
			minfee: &mockMinfeeServer{networkMinGasPrice: math.LegacyMustNewDecFromStr("0.01")},
			want:   0.01,
		},
		{
			name:   "local price above network price",
			minfee: &mockMinfeeServer{networkMinGasPrice: math.LegacyMustNewDecFromStr("0.001")},
			want:   0.004,
		},
		{
			name:   "minfee service not available",
			minfee: nil,
			want:   0.004,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			conn := newMinGasPriceConn(t, "0.004utia", tc.minfee)
			got, err := user.QueryMinimumGasPrice(context.Background(), conn)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
