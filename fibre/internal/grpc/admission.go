package grpc

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
)

type admission struct {
	disabled         bool
	mu               sync.Mutex
	total, reads     int
	limit, readLimit int
}

func (a *admission) acquire(upload bool) (*rpcSlot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.disabled && (a.total == a.limit || (!upload && a.reads == a.readLimit)) {
		return nil, status.Error(codes.ResourceExhausted, "fibre server busy; retry later")
	}
	a.total++
	if !upload {
		a.reads++
	}
	slot := &rpcSlot{admission: a, upload: upload}
	slot.refs.Store(1)
	return slot, nil
}

type rpcSlot struct {
	admission *admission
	upload    bool
	refs      atomic.Int32
}

func (s *rpcSlot) release() {
	if s.refs.Add(-1) != 0 {
		return
	}
	a := s.admission
	a.mu.Lock()
	a.total--
	if !s.upload {
		a.reads--
	}
	a.mu.Unlock()
}

// register keeps unary cardinality and wire messages, but acquires before RecvMsg.
func (a *admission) register(server *grpc.Server, service types.FibreServer) {
	desc := types.Fibre_serviceDesc
	desc.Methods = nil
	desc.Streams = nil
	for _, method := range types.Fibre_serviceDesc.Methods {
		desc.Streams = append(desc.Streams, grpc.StreamDesc{
			StreamName: method.MethodName,
			Handler: func(srv any, stream grpc.ServerStream) error {
				_, err := recoverUnaryInterceptor(stream.Context(), nil, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + desc.ServiceName + "/" + method.MethodName}, func(context.Context, any) (any, error) {
					return nil, a.serve(srv, stream, method)
				})
				return err
			},
		})
	}
	server.RegisterService(&desc, service)
}

func (a *admission) serve(service any, stream grpc.ServerStream, method grpc.MethodDesc) error {
	slot, err := a.acquire(method.MethodName == "UploadShard")
	if err != nil {
		return err
	}
	defer slot.release()
	// Compression would replace the buffer whose references own the slot.
	if err := grpc.SetSendCompressor(stream.Context(), encoding.Identity); err != nil {
		return err
	}
	var upload *types.UploadShardRequest
	defer func() {
		if upload != nil {
			*upload = types.UploadShardRequest{}
		}
	}()
	response, err := method.Handler(service, stream.Context(), func(v any) error {
		upload, _ = v.(*types.UploadShardRequest)
		return stream.RecvMsg(v)
	}, nil)
	if err != nil {
		return err
	}
	return stream.SendMsg(&rpcResponse{message: response.(sizedBufferMarshaler), slot: slot})
}

type rpcResponse struct {
	message sizedBufferMarshaler
	slot    *rpcSlot
}

func (r *rpcResponse) marshal() (out mem.BufferSlice, err error) {
	defer func() { r.message = nil }()
	size := r.message.Size()
	if size == 0 {
		return mem.BufferSlice{}, nil
	}
	capacity := size
	for mem.IsBelowBufferPoolingThreshold(capacity) {
		capacity *= 2
	}
	pool := &responsePool{slot: r.slot}
	buf := pool.Get(capacity)
	*buf = (*buf)[:size]
	if _, err := r.message.MarshalToSizedBuffer(*buf); err != nil {
		return nil, err
	}
	r.slot.refs.Add(1)
	return mem.BufferSlice{mem.NewBuffer(buf, pool)}, nil
}

// Response payloads are not cached after their last transport reference ends.
type responsePool struct {
	mem.NopBufferPool
	slot *rpcSlot
}

func (p *responsePool) Put(buf *[]byte) {
	*buf = nil
	p.slot.release()
}
