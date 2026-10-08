package grpc

import (
	"context"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
)

// Admission accounts for RPC working memory, including queued responses.
type Admission struct {
	*memoryBudget
	maxMessage int
	codec      *pooledCodec
	// DownloadSize estimates stored payload bytes without reading the payload. Set it before serving.
	DownloadSize func(context.Context, []byte) (int64, error)
}

// NewAdmission reserves uploadReserve bytes exclusively for uploads. Zero total disables admission.
func NewAdmission(total, uploadReserve int64, maxMessage, maxRows, maxProofs int) *Admission {
	return &Admission{
		memoryBudget: newMemoryBudget(total, uploadReserve), maxMessage: maxMessage,
		codec: NewServerCodec(maxRows, maxProofs).(*pooledCodec),
	}
}

// register adapts unary Fibre methods so admission can run before gRPC receives their bodies.
func (a *Admission) register(server *grpc.Server, service types.FibreServer, interceptor grpc.UnaryServerInterceptor) {
	desc := types.Fibre_serviceDesc
	desc.Methods = nil
	for _, method := range types.Fibre_serviceDesc.Methods {
		desc.Streams = append(desc.Streams, grpc.StreamDesc{
			StreamName: method.MethodName,
			Handler:    a.streamHandler(method, interceptor),
		})
	}
	server.RegisterService(&desc, service)
}

// streamHandler binds the generated unary method to an admitted stream.
func (a *Admission) streamHandler(method grpc.MethodDesc, interceptor grpc.UnaryServerInterceptor) grpc.StreamHandler {
	return func(srv any, stream grpc.ServerStream) error {
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/celestia.fibre.v1.Fibre/" + method.MethodName}
		_, err := recoverUnaryInterceptor(stream.Context(), nil, info, func(context.Context, any) (any, error) {
			return nil, a.serve(srv, stream, method, interceptor)
		})
		return err
	}
}

// serve holds one reservation from request receipt until the response buffers are released.
func (a *Admission) serve(srv any, stream grpc.ServerStream, method grpc.MethodDesc, interceptor grpc.UnaryServerInterceptor) error {
	lease := &memoryLease{budget: a.memoryBudget, download: method.MethodName == "DownloadShard"}
	lease.refs.Store(1)
	defer lease.release()
	ctx := stream.Context()
	if err := grpc.SetSendCompressor(ctx, encoding.Identity); err != nil {
		return err
	}
	response, err := method.Handler(srv, ctx, func(request any) error {
		return a.receive(ctx, request, lease)
	}, interceptor)
	if err != nil {
		return err
	}
	return a.sendResponse(stream, response, lease)
}

// sendResponse transfers encoded-buffer ownership to gRPC, which releases the lease after its last use.
func (a *Admission) sendResponse(stream grpc.ServerStream, response any, lease *memoryLease) error {
	transport, ok := grpc.ServerTransportStreamFromContext(stream.Context()).(transportStream)
	if !ok {
		return status.Error(codes.Internal, "gRPC transport stream unavailable")
	}
	// Interceptors may change compression; reject it if headers already prevent restoring identity.
	if transport.SendCompress() != encoding.Identity {
		if err := grpc.SetSendCompressor(stream.Context(), encoding.Identity); err != nil {
			return status.Errorf(codes.Internal, "response must be uncompressed: %v", err)
		}
	}
	msg, ok := response.(sizedBufferMarshaler)
	if !ok {
		return status.Error(codes.Internal, "unsupported response type")
	}
	if msg.Size() > a.maxMessage {
		return status.Error(codes.ResourceExhausted, "response exceeds protocol limit")
	}
	codec := &pooledCodec{pool: lease}
	data, err := codec.Marshal(response)
	if err != nil {
		return err
	}
	defer func() { data.Free() }()
	return stream.SendMsg(&data)
}
