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

// createMockServer creates a single mock gRPC server with the given configuration
func createMockServer(t *testing.T, txStatusResponses map[string][]*tx.TxStatusResponse, broadcastHandler BroadcastHandler) *grpc.ClientConn {
	mockServer := &mockTxServer{
		txStatusResponses:   txStatusResponses,
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

	return conn
}

// setupTxClientWithMockServers creates mock gRPC servers with different broadcast handlers (works for single or multiple servers)
func setupTxClientWithMockServers(t *testing.T, broadcastHandlers []BroadcastHandler, txStatusResponses map[string][]*tx.TxStatusResponse, opts ...user.Option) (*user.TxClient, []*grpc.ClientConn) {
	conns := make([]*grpc.ClientConn, 0, len(broadcastHandlers))

	for i, handler := range broadcastHandlers {
		// Use provided txStatusResponses for first server, empty for others
		var responses map[string][]*tx.TxStatusResponse
		if i == 0 && txStatusResponses != nil {
			responses = txStatusResponses
		} else {
			responses = make(map[string][]*tx.TxStatusResponse)
		}
		conn := createMockServer(t, responses, handler)
		conns = append(conns, conn)
	}

	primaryConn := conns[0]
	var otherConns []*grpc.ClientConn
	if len(conns) > 1 {
		otherConns = conns[1:]
	}

	// Create TxClient with mock connection
	encCfg, txClient, _ := setupTxClientWithDefaultParams(t)

	// Build options list
	clientOpts := opts
	if len(otherConns) > 0 {
		clientOpts = append(clientOpts, user.WithAdditionalCoreEndpoints(otherConns))
	}

	mockTxClient, err := user.NewTxClient(
		encCfg.Codec,
		txClient.Signer(),
		primaryConn,
		encCfg.InterfaceRegistry,
		clientOpts...,
	)
	require.NoError(t, err)

	return mockTxClient, conns
}
