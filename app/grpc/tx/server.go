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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

// maxTxStatusBatchIDs is the maximum number of tx ids a TxStatusBatch request
// may carry.
const maxTxStatusBatchIDs = 20

// txStatusBatchTxIDsField is the field number of TxStatusBatchRequest.tx_ids.
const txStatusBatchTxIDsField = 1

// RegisterTxService registers the tx service on the gRPC router.
func RegisterTxService(
	qrt gogogrpc.Server,
	clientCtx client.Context,
	interfaceRegistry codectypes.InterfaceRegistry,
) {
	qrt.RegisterService(txServiceDesc(), NewTxServer(clientCtx, interfaceRegistry))
}

// txServiceDesc returns the generated Tx service description with the
// TxStatusBatch handler replaced by one that enforces the tx id limit while
// decoding the request, before any tx ids are allocated.
func txServiceDesc() *grpc.ServiceDesc {
	desc := _Tx_serviceDesc
	desc.Methods = append([]grpc.MethodDesc(nil), _Tx_serviceDesc.Methods...)
	for i := range desc.Methods {
		if desc.Methods[i].MethodName == "TxStatusBatch" {
			desc.Methods[i].Handler = boundedTxStatusBatchHandler
		}
	}
	return &desc
}

// boundedTxStatusBatchHandler wraps the generated TxStatusBatch handler so
// that the request is decoded through boundedTxStatusBatchRequest.
func boundedTxStatusBatchHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	return _Tx_TxStatusBatch_Handler(srv, ctx, func(in any) error {
		req, ok := in.(*TxStatusBatchRequest)
		if !ok {
			return dec(in)
		}
		bounded := &boundedTxStatusBatchRequest{req: req}
		if err := dec(bounded); err != nil {
			return err
		}
		if bounded.tooManyIDs {
			return errTooManyTxIDs()
		}
		return nil
	}, interceptor)
}

// boundedTxStatusBatchRequest decodes into req only if the encoded request
// carries at most maxTxStatusBatchIDs tx ids. Otherwise it leaves req empty and
// sets tooManyIDs without failing the decode, because the gRPC server reports
// every decode error as Internal and the limit error should stay
// InvalidArgument.
type boundedTxStatusBatchRequest struct {
	req        *TxStatusBatchRequest
	tooManyIDs bool
}

func (m *boundedTxStatusBatchRequest) Reset()         { m.req.Reset() }
func (m *boundedTxStatusBatchRequest) String() string { return m.req.String() }
func (*boundedTxStatusBatchRequest) ProtoMessage()    {}

func (m *boundedTxStatusBatchRequest) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}

func (m *boundedTxStatusBatchRequest) Unmarshal(b []byte) error {
	if tooManyTxStatusBatchIDs(b) {
		m.tooManyIDs = true
		return nil
	}
	return m.req.Unmarshal(b)
}

func errTooManyTxIDs() error {
	return status.Errorf(codes.InvalidArgument, "maximum of %d tx ids allowed", maxTxStatusBatchIDs)
}

// tooManyTxStatusBatchIDs scans an encoded TxStatusBatchRequest without
// allocating and reports whether it carries more than maxTxStatusBatchIDs tx
// ids. Malformed input is left for the regular decoder to reject.
func tooManyTxStatusBatchIDs(b []byte) bool {
	count := 0
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return false
		}
		b = b[n:]
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return false
		}
		b = b[n:]
		if num == txStatusBatchTxIDsField {
			count++
			if count > maxTxStatusBatchIDs {
				return true
			}
		}
	}
	return false
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

	if len(req.TxIds) > maxTxStatusBatchIDs {
		return nil, errTooManyTxIDs()
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
