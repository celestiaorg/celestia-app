package user_test

import (
	"context"
	"net"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/rpc/core"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// BroadcastHandler is a function type for handling broadcast requests
type BroadcastHandler func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error)

// mockTxServer implements both gRPC ServiceServer and TxServer interfaces for mocking broadcast and tx status responses
type mockTxServer struct {
	sdktx.UnimplementedServiceServer
	tx.UnimplementedTxServer
	txStatusResponses   map[string][]*tx.TxStatusResponse // txHash with sequence of responses
	txStatusCallCounts  map[string]int                    // txHash with number of TxStatus calls made
	broadcastCallCounts map[string]int                    // txHash with number of BroadcastTx calls made
	broadcastHandler    BroadcastHandler                  // Custom broadcast handler
}

func (m *mockTxServer) TxStatus(ctx context.Context, req *tx.TxStatusRequest) (*tx.TxStatusResponse, error) {
	if responses, exists := m.txStatusResponses[req.TxId]; exists {
		callCount := m.txStatusCallCounts[req.TxId]
		m.txStatusCallCounts[req.TxId]++

		// Return response based on call count
		if callCount < len(responses) {
			resp := responses[callCount]
			return resp, nil
		}
		// If we've exhausted predefined responses, return the last response
		lastResp := responses[len(responses)-1]
		return lastResp, nil
	}

	// Default response
	return &tx.TxStatusResponse{
		Status: core.TxStatusPending,
		Height: 1,
	}, nil
}

func (m *mockTxServer) BroadcastTx(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
	// Use custom handler if provided
	if m.broadcastHandler != nil {
		return m.broadcastHandler(ctx, req)
	}

	// Default behavior: Same hash for all broadcast calls
	txHash := "test-tx-hash-123"

	// Increment broadcast call count
	m.broadcastCallCounts[txHash]++

	// When it's called for the second time return sequence mismatch error
	if m.broadcastCallCounts[txHash] > 1 {
		return &sdktx.BroadcastTxResponse{
			TxResponse: &sdk.TxResponse{
				TxHash: txHash,
				Code:   32, // sequence mismatch error code
				Height: 1,
				RawLog: "account sequence mismatch",
			},
		}, nil
	}

	// Return successful broadcast response
	return &sdktx.BroadcastTxResponse{
		TxResponse: &sdk.TxResponse{
			TxHash: txHash,
			Code:   abci.CodeTypeOK,
			Height: 1,
			Data:   "",
			RawLog: "",
		},
	}, nil
}

// setupTxClientWithMockGRPCServer creates a TxClient connected to a mock gRPC server that lets you mock broadcast and tx status responses
func setupTxClientWithMockGRPCServer(t *testing.T, responseSequences map[string][]*tx.TxStatusResponse, opts ...user.Option) (*user.TxClient, *grpc.ClientConn) {
	return setupTxClientWithMockGRPCServerAndBroadcastHandler(t, responseSequences, nil, opts...)
}

// setupTxClientWithMockGRPCServerAndBroadcastHandler creates a TxClient connected to a mock gRPC server with custom broadcast handler
func setupTxClientWithMockGRPCServerAndBroadcastHandler(t *testing.T, responseSequences map[string][]*tx.TxStatusResponse, broadcastHandler BroadcastHandler, opts ...user.Option) (*user.TxClient, *grpc.ClientConn) {
	// Create mock server with provided response sequences
	mockServer := &mockTxServer{
		txStatusResponses:   responseSequences,
		txStatusCallCounts:  make(map[string]int),
		broadcastCallCounts: make(map[string]int),
		broadcastHandler:    broadcastHandler,
	}

	// Set up in-memory gRPC server
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	sdktx.RegisterServiceServer(s, mockServer) // For BroadcastTx
	tx.RegisterTxServer(s, mockServer)         // For TxStatus

	go func() {
		if err := s.Serve(lis); err != nil {
			t.Logf("Server exited with error: %v", err)
		}
	}()

	// Create client connection
	//nolint:staticcheck
	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	// Create TxClient with mock connection
	encCfg, txClient, _ := setupTxClientWithDefaultParams(t)

	mockTxClient, err := user.NewTxClient(
		encCfg.Codec,
		txClient.Signer(),
		conn,
		encCfg.InterfaceRegistry,
		opts...,
	)
	require.NoError(t, err)

	return mockTxClient, conn
}

// setupMultipleMockServers creates multiple mock gRPC servers with different broadcast handlers
func setupMultipleMockServers(t *testing.T, broadcastHandlers []BroadcastHandler) []*grpc.ClientConn {
	var conns []*grpc.ClientConn

	for _, handler := range broadcastHandlers {
		// Create mock server with custom broadcast handler
		mockServer := &mockTxServer{
			txStatusResponses:   make(map[string][]*tx.TxStatusResponse),
			txStatusCallCounts:  make(map[string]int),
			broadcastCallCounts: make(map[string]int),
			broadcastHandler:    handler,
		}

		// Set up in-memory gRPC server
		lis := bufconn.Listen(1024 * 1024)
		s := grpc.NewServer()
		sdktx.RegisterServiceServer(s, mockServer) // For BroadcastTx
		tx.RegisterTxServer(s, mockServer)         // For TxStatus

		go func() {
			if err := s.Serve(lis); err != nil {
				t.Logf("Server exited with error: %v", err)
			}
		}()

		// Create client connection
		//nolint:staticcheck
		conn, err := grpc.DialContext(context.Background(), "bufnet",
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return lis.Dial()
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		require.NoError(t, err)

		conns = append(conns, conn)
	}

	return conns
}
