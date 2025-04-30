package grpctest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
)

// MockTxService allows controlling the behavior of BroadcastTx calls.
type MockTxService struct {
	sdktx.UnimplementedServiceServer // Embed the unimplemented server

	BroadcastHandler func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error)
	Invocations      atomic.Int32 // Use atomic for potential concurrent access if needed
}

func (m *MockTxService) BroadcastTx(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
	m.Invocations.Add(1)
	if m.BroadcastHandler != nil {
		return m.BroadcastHandler(ctx, req)
	}
	// Default behavior if no handler is set
	return nil, fmt.Errorf("MockTxService.BroadcastHandler not set")
}

func (m *MockTxService) Simulate(context.Context, *sdktx.SimulateRequest) (*sdktx.SimulateResponse, error) {
	return nil, errors.New("Simulate not implemented in mock")
}

func (m *MockTxService) GetTx(context.Context, *sdktx.GetTxRequest) (*sdktx.GetTxResponse, error) {
	return nil, errors.New("GetTx not implemented in mock")
}

func (m *MockTxService) GetBlockWithTxs(context.Context, *sdktx.GetBlockWithTxsRequest) (*sdktx.GetBlockWithTxsResponse, error) {
	return nil, errors.New("GetBlockWithTxs not implemented in mock")
}

func (m *MockTxService) GetTxsEvent(context.Context, *sdktx.GetTxsEventRequest) (*sdktx.GetTxsEventResponse, error) {
	return nil, errors.New("GetTxsEvent not implemented in mock")
}

func (m *MockTxService) TxDecode(context.Context, *sdktx.TxDecodeRequest) (*sdktx.TxDecodeResponse, error) {
	return nil, errors.New("TxDecode not implemented in mock")
}

func (m *MockTxService) TxEncode(context.Context, *sdktx.TxEncodeRequest) (*sdktx.TxEncodeResponse, error) {
	return nil, errors.New("TxEncode not implemented in mock")
}

func (m *MockTxService) TxEncodeAmino(context.Context, *sdktx.TxEncodeAminoRequest) (*sdktx.TxEncodeAminoResponse, error) {
	return nil, errors.New("TxEncodeAmino not implemented in mock")
}

func (m *MockTxService) TxDecodeAmino(context.Context, *sdktx.TxDecodeAminoRequest) (*sdktx.TxDecodeAminoResponse, error) {
	return nil, errors.New("TxDecodeAmino not implemented in mock")
}

// StartMockServer starts a mock gRPC server with the given MockTxService using a TCP listener.
func StartMockServer(t *testing.T, service *MockTxService) (*grpc.ClientConn, func()) {
	t.Helper()

	// Get a free port using the utility function
	port, err := testnode.GetFreePort()
	require.NoError(t, err)
	addr := fmt.Sprintf("localhost:%d", port)

	// Use a real TCP listener on the dynamically obtained port
	lis, err := net.Listen("tcp", addr)
	require.NoError(t, err)

	s := grpc.NewServer()
	sdktx.RegisterServiceServer(s, service)

	go func() {
		if err := s.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Logf("Mock server error: %v", err)
			panic(err)
		}
	}()

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	stop := func() {
		s.Stop()
		closeErr := lis.Close()
		if closeErr != nil {
			t.Logf("Error closing listener: %v", closeErr)
		}
		if conn != nil {
			connCloseErr := conn.Close()
			if connCloseErr != nil {
				t.Logf("Error closing connection: %v", connCloseErr)
			}
		}
	}

	return conn, stop
}
