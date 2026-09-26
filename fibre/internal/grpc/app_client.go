package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"

	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	valtypes "github.com/celestiaorg/celestia-app/v10/x/valaddr/types"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	core "github.com/cometbft/cometbft/types"
	tmservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	_ state.Client       = (*AppClient)(nil)
	_ state.HealthClient = (*AppClient)(nil)
)

// AppClient manages a gRPC client connection to a celestia-app node
// and provides the query methods needed by the Fibre server.
type AppClient struct {
	*SetGetter
	*HostRegistry
	conn          *grpclib.ClientConn
	blockAPI      coregrpc.BlockAPIClient
	queryClient   types.QueryClient
	valaddrClient valtypes.QueryClient
	log           *slog.Logger

	chainID string // resolved on Start
}

// NewAppClient creates an [AppClient] connected to the given address.
// The underlying gRPC connection is lazy — no network I/O happens until the first RPC.
// Call [Start] to auto-detect the chain ID from the node.
// hostOpts are forwarded to the embedded [HostRegistry].
func NewAppClient(addr string, log *slog.Logger, hostOpts ...HostRegistryOption) (*AppClient, error) {
	conn, err := grpclib.NewClient(
		addr,
		grpclib.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create app gRPC client (%s): %w", addr, err)
	}

	blockAPI, valaddrClient := coregrpc.NewBlockAPIClient(conn), valtypes.NewQueryClient(conn)
	return &AppClient{
		SetGetter:     NewSetGetter(blockAPI),
		HostRegistry:  NewHostRegistry(valaddrClient, log, hostOpts...),
		conn:          conn,
		blockAPI:      blockAPI,
		queryClient:   types.NewQueryClient(conn),
		valaddrClient: valaddrClient,
		log:           log,
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
	return state.VerifiedPromise{
		ExpiresAt:      *resp.ExpirationTime,
		ShardRetention: resp.ShardRetention,
	}, nil
}

func (c *AppClient) FullStakeStorageBudget(ctx context.Context) (int64, error) {
	resp, err := c.queryClient.Params(ctx, &types.QueryParamsRequest{})
	if err != nil {
		return 0, err
	}

	budget := resp.Params.FullStakeStorageBudget
	if budget > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(budget), nil
}

// NodeStatus implements [state.HealthClient] with a fresh BlockAPI Status call.
func (c *AppClient) NodeStatus(ctx context.Context) (state.NodeStatus, error) {
	resp, err := c.blockAPI.Status(ctx, &coregrpc.StatusRequest{})
	if err != nil {
		return state.NodeStatus{}, err
	}
	if resp.GetNodeInfo() == nil || resp.GetSyncInfo() == nil || resp.SyncInfo.LatestBlockHeight < 0 {
		return state.NodeStatus{}, fmt.Errorf("incomplete status response from app node")
	}
	return state.NodeStatus{
		ChainID:    strings.TrimSpace(resp.NodeInfo.Network),
		Height:     uint64(resp.SyncInfo.LatestBlockHeight),
		BlockTime:  resp.SyncInfo.LatestBlockTime,
		CatchingUp: resp.SyncInfo.CatchingUp,
	}, nil
}

// ValidatorSetAt implements [state.HealthClient]; [SetGetter] never caches.
func (c *AppClient) ValidatorSetAt(ctx context.Context, height uint64) (validator.Set, error) {
	return c.GetByHeight(ctx, height)
}

// ProviderRegistration implements [state.HealthClient], bypassing the [HostRegistry] cache.
func (c *AppClient) ProviderRegistration(ctx context.Context, addr core.Address) (state.ProviderRegistration, error) {
	resp, err := c.valaddrClient.FibreProviderInfo(ctx, &valtypes.QueryFibreProviderInfoRequest{
		ValidatorConsensusAddress: sdk.ConsAddress(addr.Bytes()).String(),
	})
	if err != nil {
		return state.ProviderRegistration{}, err
	}
	if !resp.GetFound() || resp.Info == nil {
		return state.ProviderRegistration{}, nil
	}
	return state.ProviderRegistration{Found: true, Host: resp.Info.Host}, nil
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
