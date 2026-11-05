package tx

import (
	"context"
	"encoding/hex"

	rpcclient "github.com/cometbft/cometbft/rpc/client"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	gogogrpc "github.com/cosmos/gogoproto/grpc"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	if resTx == nil {
		return nil, status.Error(codes.Internal, "node returned an empty tx status")
	}

	return newTxStatusResponse(resTx), nil
}

func (s *txServer) TxStatusBatch(ctx context.Context, req *TxStatusBatchRequest) (*TxStatusBatchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	if len(req.TxIds) > 20 {
		return nil, status.Error(codes.InvalidArgument, "maximum of 20 tx ids allowed")
	}

	if len(req.TxIds) == 0 {
		return nil, status.Error(codes.InvalidArgument, "tx ids cannot be empty")
	}

	node, err := s.clientCtx.GetNode()
	if err != nil {
		return nil, err
	}

	nodeTxStatus, ok := node.(rpcclient.SignClient)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "node does not support tx status batch")
	}

	txIDs := make([][]byte, len(req.TxIds))
	for i, txId := range req.TxIds {
		if len(txId) == 0 {
			return nil, status.Error(codes.InvalidArgument, "tx id cannot be empty")
		}
		txID, err := hex.DecodeString(txId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid tx id: %s", err)
		}
		txIDs[i] = txID
	}

	txStatusBatchResponses, err := nodeTxStatus.TxStatusBatch(ctx, txIDs)
	if err != nil {
		return nil, err
	}
	if txStatusBatchResponses == nil {
		return nil, status.Error(codes.Internal, "node returned an empty tx status batch")
	}
	if len(txStatusBatchResponses.Statuses) != len(txIDs) {
		return nil, status.Errorf(codes.Internal, "node returned %d tx statuses for %d tx ids", len(txStatusBatchResponses.Statuses), len(txIDs))
	}

	// Match results to the requested ids by hash rather than by position so
	// that a node that reorders or substitutes results cannot attach one tx's
	// status to another tx's id.
	requested := make(map[string]struct{}, len(txIDs))
	for _, txID := range txIDs {
		requested[string(txID)] = struct{}{}
	}
	results := make(map[string]*ctypes.ResultTxStatus, len(txIDs))
	for i := range txStatusBatchResponses.Statuses {
		res := &txStatusBatchResponses.Statuses[i]
		if _, ok := requested[string(res.Hash)]; !ok {
			return nil, status.Errorf(codes.Internal, "node returned a tx status for unrequested hash %X", []byte(res.Hash))
		}
		results[string(res.Hash)] = &res.Result
	}

	responses := make([]*TxStatusResult, len(txIDs))
	for i, txID := range txIDs {
		res, ok := results[string(txID)]
		if !ok {
			return nil, status.Errorf(codes.Internal, "node returned no tx status for tx id %s", req.TxIds[i])
		}
		responses[i] = &TxStatusResult{
			TxHash: req.TxIds[i],
			Status: newTxStatusResponse(res),
		}
	}

	return &TxStatusBatchResponse{
		Statuses: responses,
	}, nil
}

func newTxStatusResponse(res *ctypes.ResultTxStatus) *TxStatusResponse {
	return &TxStatusResponse{
		Height:        res.Height,
		Index:         res.Index,
		ExecutionCode: res.ExecutionCode,
		Error:         res.Error,
		Status:        res.Status,
		GasWanted:     res.GasWanted,
		GasUsed:       res.GasUsed,
		Codespace:     res.Codespace,
		Signers:       res.Signers,
	}
}
