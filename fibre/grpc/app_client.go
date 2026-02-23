package grpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	tmservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// AppClient manages a gRPC client connection to a celestia-app node
// and provides the query methods needed by the Fibre server.
type AppClient struct {
	*SetGetter
	conn        *grpclib.ClientConn
	queryClient types.QueryClient

	chainID string // resolved on Start
}

// NewAppClient creates an [AppClient] connected to the given address.
// The underlying gRPC connection is lazy — no network I/O happens until the first RPC.
// Call [Start] to auto-detect the chain ID from the node.
func NewAppClient(addr string) (*AppClient, error) {
	// grpc.NewClient is lazy — no network I/O happens here, so this is safe outside of Start.
	conn, err := grpclib.NewClient(
		addr,
		grpclib.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create app gRPC client (%s): %w", addr, err)
	}

	return &AppClient{
		SetGetter:   NewSetGetter(coregrpc.NewBlockAPIClient(conn)),
		conn:        conn,
		queryClient: types.NewQueryClient(conn),
	}, nil
}

// Start connects to the app node and resolves the chain ID.
func (c *AppClient) Start(ctx context.Context) error {
	chainID, err := detectChainID(ctx, c.conn)
	if err != nil {
		return fmt.Errorf("detect chain ID: %w", err)
	}
	c.chainID = chainID
	return nil
}

// Stop closes the underlying gRPC connection.
func (c *AppClient) Stop() error {
	return c.conn.Close()
}

// ChainID returns the chain ID resolved during [Start].
func (c *AppClient) ChainID() string { return c.chainID }

// Verify validates a payment promise against on-chain state and returns the expiration time.
func (c *AppClient) Verify(ctx context.Context, promise *types.PaymentPromise) (time.Time, error) {
	resp, err := c.queryClient.ValidatePaymentPromise(ctx, &types.QueryValidatePaymentPromiseRequest{Promise: *promise})
	if err != nil {
		return time.Time{}, err
	}
	if !resp.IsValid {
		return time.Time{}, fmt.Errorf("payment promise is invalid")
	}
	if resp.ExpirationTime == nil {
		return time.Time{}, fmt.Errorf("expiration time not provided in validation response")
	}
	return *resp.ExpirationTime, nil
}

func detectChainID(ctx context.Context, conn *grpclib.ClientConn) (string, error) {
	resp, err := tmservice.NewServiceClient(conn).GetNodeInfo(ctx, &tmservice.GetNodeInfoRequest{})
	if err != nil {
		return "", err
	}
	if resp == nil || resp.DefaultNodeInfo == nil {
		return "", fmt.Errorf("missing node info in gRPC response")
	}

	chainID := strings.TrimSpace(resp.DefaultNodeInfo.Network)
	if chainID == "" {
		return "", fmt.Errorf("empty chain ID in node info response")
	}
	return chainID, nil
}
