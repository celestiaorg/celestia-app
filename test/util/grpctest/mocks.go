package grpctest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const (
	bufferSize = 1024 * 1024
)

// MockTxService allows controlling the behavior of BroadcastTx calls.
// Note: Renamed from mockTxService to avoid stuttering (grpctest.MockTxService).
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

// StartMockServer starts a mock gRPC server with the given MockTxService using bufconn.
// Note: Renamed from startMockServer to be exported.
func StartMockServer(t *testing.T, service *MockTxService) (*grpc.ClientConn, func()) {
	t.Helper()
	lis := bufconn.Listen(bufferSize)
	s := grpc.NewServer()
	sdktx.RegisterServiceServer(s, service)

	go func() {
		if err := s.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			// We don't fail the test here as closing the listener causes Serve to return an error
			fmt.Printf("Mock server error: %v\n", err)
		}
	}()

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	stop := func() {
		s.Stop()
		err := lis.Close()
		// Tolerate EBADF which can happen if the connection is already closed
		if err != nil && !errors.Is(err, net.ErrClosed) && !strings.Contains(err.Error(), "use of closed network connection") && !strings.Contains(err.Error(), "bad file descriptor") {
			fmt.Printf("Error closing listener: %v\n", err)
		}
		err = conn.Close()
		if err != nil {
			fmt.Printf("Error closing connection: %v\n", err)
		}
	}

	return conn, stop
}
