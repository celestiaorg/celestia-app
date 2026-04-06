package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/celestiaorg/celestia-app/v8/fibre/state"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	valtypes "github.com/celestiaorg/celestia-app/v8/x/valaddr/types"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	tmservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var _ state.Client = (*AppClient)(nil)

// AppClient manages a gRPC client connection to a celestia-app node
// and provides the query methods needed by the Fibre server.
type AppClient struct {
	*SetGetter
	*HostRegistry
	conn        *grpclib.ClientConn
	queryClient types.QueryClient
	log         *slog.Logger

	chainID string // resolved on Start
}

// NewAppClient creates an [AppClient] connected to the given address.
// The underlying gRPC connection is lazy — no network I/O happens until the first RPC.
// Call [Start] to auto-detect the chain ID from the node.
func NewAppClient(addr string, log *slog.Logger) (*AppClient, error) {
	conn, err := grpclib.NewClient(
		addr,
		grpclib.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create app gRPC client (%s): %w", addr, err)
	}

	return &AppClient{
		SetGetter:    NewSetGetter(coregrpc.NewBlockAPIClient(conn)),
		HostRegistry: NewHostRegistry(valtypes.NewQueryClient(conn), log),
		conn:         conn,
		queryClient:  types.NewQueryClient(conn),
		log:          log,
	}, nil
}

// Start connects to the app node, resolves the chain ID and pulls
// the host registry in parallel.
func (c *AppClient) Start(ctx context.Context) error {
	var (
		chainID  string
		chainErr error
		hostErr  error
	)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		chainID, chainErr = detectChainID(ctx, c.conn)
	}()
	go func() {
		defer wg.Done()
		hostErr = c.HostRegistry.Start(ctx)
	}()
	wg.Wait()

	if chainErr != nil {
		return fmt.Errorf("detect chain ID: %w", chainErr)
	}
	if hostErr != nil {
		return fmt.Errorf("start host registry: %w", hostErr)
	}
	c.chainID = chainID
	c.log.Info("connected to app node", "chain_id", chainID)
	return nil
}

// Stop closes the underlying gRPC connection.
func (c *AppClient) Stop(_ context.Context) error {
	c.log.Info("disconnected from app node")
	return c.conn.Close()
}

// ChainID returns the chain ID resolved during [Start].
func (c *AppClient) ChainID() string { return c.chainID }

// VerifyPromise validates a payment promise against on-chain state and returns the verification result.
func (c *AppClient) VerifyPromise(ctx context.Context, promise *state.PaymentPromise) (state.VerifiedPromise, error) {
	resp, err := c.queryClient.ValidatePaymentPromise(ctx, &types.QueryValidatePaymentPromiseRequest{Promise: *promise})
	if err != nil {
		return state.VerifiedPromise{}, err
	}
	if !resp.IsValid {
		return state.VerifiedPromise{}, fmt.Errorf("payment promise is invalid")
	}
	if resp.ExpirationTime == nil {
		return state.VerifiedPromise{}, fmt.Errorf("expiration time not provided in validation response")
	}
	return state.VerifiedPromise{ExpiresAt: *resp.ExpirationTime}, nil
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
