package tx

import (
	"context"
	"encoding/hex"

	rpcclient "github.com/cometbft/cometbft/rpc/client"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	gogogrpc "github.com/cosmos/gogoproto/grpc"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// RegisterTxService registers the tx service on the gRPC router.
func RegisterTxService(
	qrt gogogrpc.Server,
	clientCtx client.Context,
	interfaceRegistry codectypes.InterfaceRegistry,
) {
	RegisterTxServer(
		qrt,
		NewTxServer(clientCtx, interfaceRegistry),
	)
}

// RegisterGRPCGatewayRoutes mounts the tx service's GRPC-gateway routes on the
// given Mux.
func RegisterGRPCGatewayRoutes(clientConn gogogrpc.ClientConn, mux *runtime.ServeMux) {
	err := RegisterTxHandlerClient(context.Background(), mux, NewTxClient(clientConn))
	if err != nil {
		panic(err)
	}
}

var _ TxServer = &txServer{}

type txServer struct {
	clientCtx         client.Context
	interfaceRegistry codectypes.InterfaceRegistry
}

func NewTxServer(clientCtx client.Context, interfaceRegistry codectypes.InterfaceRegistry) TxServer {
	return &txServer{
		clientCtx:         clientCtx,
		interfaceRegistry: interfaceRegistry,
	}
}

// TxStatus implements the TxServer.TxStatus method proxying to the underlying celestia-core RPC server
func (s *txServer) TxStatus(ctx context.Context, req *TxStatusRequest) (*TxStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	if len(req.TxId) == 0 {
		return nil, status.Error(codes.InvalidArgument, "tx id cannot be empty")
	}

	node, err := s.clientCtx.GetNode()
	if err != nil {
		return nil, err
	}

	nodeTxStatus, ok := node.(rpcclient.SignClient)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "node does not support tx status")
	}

	txID, err := hex.DecodeString(req.TxId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid tx id: %s", err)
	}

	resTx, err := nodeTxStatus.TxStatus(ctx, txID)
	if err != nil {
		return nil, err
	}

	return &TxStatusResponse{
		Height:        resTx.Height,
		Index:         resTx.Index,
		ExecutionCode: resTx.ExecutionCode,
		Error:         resTx.Error,
		Status:        resTx.Status,
	}, nil
}
