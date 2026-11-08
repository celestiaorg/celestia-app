package tx

import (
	"context"
	"encoding/hex"

	rpcclient "github.com/cometbft/cometbft/rpc/client"
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

// boundedTxStatusBatchHandler replaces the generated TxStatusBatch handler.
// It decodes the request through boundedTxStatusBatchRequest and, if the
// request was not decoded, returns the rejection from inside the
// interceptor-wrapped handler so that interceptors still see the request.
func boundedTxStatusBatchHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(TxStatusBatchRequest)
	bounded := &boundedTxStatusBatchRequest{req: in}
	if err := dec(bounded); err != nil {
		return nil, err
	}
	handler := func(ctx context.Context, req any) (any, error) {
		if bounded.rejectErr != nil {
			return nil, bounded.rejectErr
		}
		return srv.(TxServer).TxStatusBatch(ctx, req.(*TxStatusBatchRequest))
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/celestia.core.v1.tx.Tx/TxStatusBatch",
	}
	return interceptor(ctx, in, info, handler)
}

// boundedTxStatusBatchRequest decodes into req only if the encoded request is
// well formed and carries at most maxTxStatusBatchIDs tx ids. Otherwise it
// leaves req empty and records the rejection in rejectErr without failing the
// decode, because the gRPC server reports every decode error as Internal and
// skips interceptors for it.
type boundedTxStatusBatchRequest struct {
	req       *TxStatusBatchRequest
	rejectErr error
}

func (m *boundedTxStatusBatchRequest) Reset()         { m.req.Reset() }
func (m *boundedTxStatusBatchRequest) String() string { return m.req.String() }
func (*boundedTxStatusBatchRequest) ProtoMessage()    {}

func (m *boundedTxStatusBatchRequest) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}

func (m *boundedTxStatusBatchRequest) Unmarshal(b []byte) error {
	if err := checkTxStatusBatchRequest(b); err != nil {
		m.rejectErr = err
		return nil
	}
	return m.req.Unmarshal(b)
}

func errTooManyTxIDs() error {
	return status.Errorf(codes.InvalidArgument, "maximum of %d tx ids allowed", maxTxStatusBatchIDs)
}

// checkTxStatusBatchRequest scans an encoded TxStatusBatchRequest without
// allocating. It rejects input that is not valid protobuf, which the generated
// decoder may still partly accept, and input that carries more than
// maxTxStatusBatchIDs tx ids.
func checkTxStatusBatchRequest(b []byte) error {
	count := 0
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return errInvalidTxStatusBatchRequest(protowire.ParseError(n))
		}
		b = b[n:]
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return errInvalidTxStatusBatchRequest(protowire.ParseError(n))
		}
		b = b[n:]
		if num == txStatusBatchTxIDsField {
			count++
			if count > maxTxStatusBatchIDs {
				return errTooManyTxIDs()
			}
		}
	}
	return nil
}

func errInvalidTxStatusBatchRequest(err error) error {
	return status.Errorf(codes.InvalidArgument, "invalid tx status batch request: %v", err)
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
		GasWanted:     resTx.GasWanted,
		GasUsed:       resTx.GasUsed,
		Codespace:     resTx.Codespace,
		Signers:       resTx.Signers,
	}, nil
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

	responses := make([]*TxStatusResult, len(txStatusBatchResponses.Statuses))
	for i, status := range txStatusBatchResponses.Statuses {
		responses[i] = &TxStatusResult{
			TxHash: req.TxIds[i],
			Status: &TxStatusResponse{
				Height:        status.Result.Height,
				Index:         status.Result.Index,
				ExecutionCode: status.Result.ExecutionCode,
				Error:         status.Result.Error,
				Status:        status.Result.Status,
				GasWanted:     status.Result.GasWanted,
				GasUsed:       status.Result.GasUsed,
				Codespace:     status.Result.Codespace,
				Signers:       status.Result.Signers,
			},
		}
	}

	return &TxStatusBatchResponse{
		Statuses: responses,
	}, nil
}
