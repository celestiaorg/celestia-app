package grpc

import (
	"context"
	"sync"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc"
)

const minUploadBufferSize = 1 << 20

// uploadBuffers retains at most limit idle bytes. Active buffers are bounded by
// the server's receive size and concurrency limits, and never shared by requests.
type uploadBuffers struct {
	mu        sync.Mutex
	limit     int
	idle      [][]byte
	idleBytes int
	active    map[*types.UploadShardRequest][]byte
}

func (p *uploadBuffers) get(size int) []byte {
	p.mu.Lock()
	for i, buf := range p.idle {
		if cap(buf) >= size {
			p.idleBytes -= cap(buf)
			p.idle[i] = p.idle[len(p.idle)-1]
			p.idle[len(p.idle)-1] = nil
			p.idle = p.idle[:len(p.idle)-1]
			p.mu.Unlock()
			return buf[:size]
		}
	}
	p.mu.Unlock()
	return make([]byte, size)
}

func (p *uploadBuffers) put(buf []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cap(buf) >= minUploadBufferSize && cap(buf) <= p.limit-p.idleBytes {
		p.idle = append(p.idle, buf)
		p.idleBytes += cap(buf)
	}
}

func (p *uploadBuffers) retain(req *types.UploadShardRequest, buf []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active == nil {
		p.active = make(map[*types.UploadShardRequest][]byte)
	}
	p.active[req] = buf
}

func (p *uploadBuffers) release(req *types.UploadShardRequest) {
	p.mu.Lock()
	buf := p.active[req]
	delete(p.active, req)
	p.mu.Unlock()
	if buf != nil {
		p.put(buf)
	}
}

func (p *uploadBuffers) intercept(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if upload, ok := req.(*types.UploadShardRequest); ok {
		defer p.release(upload)
	}
	return handler(ctx, req)
}

// RegisterWithUploadBufferReuse installs bounded upload decoding and RPC-scoped
// backing buffers. Handlers and interceptors must finish reading requests before
// returning; payload-retaining stats handlers must not be installed.
// The cleanup interceptor is outermost; opts must not set grpc.UnaryInterceptor.
func (s *Server) RegisterWithUploadBufferReuse(service types.FibreServer, maxMessageSize, maxShardRows, maxProofSegments int, opts ...grpc.ServerOption) {
	codec := NewServerCodec(maxShardRows, maxProofSegments).(*pooledCodec)
	if maxMessageSize <= 0 {
		panic("fibre-proto codec: max message size must be positive")
	}
	// gRPC tracing lazily formats messages after the RPC has returned.
	if !grpc.EnableTracing {
		codec.uploads = &uploadBuffers{limit: maxMessageSize}
		opts = append(opts, grpc.UnaryInterceptor(codec.uploads.intercept))
	}
	opts = append(opts, grpc.MaxRecvMsgSize(maxMessageSize), grpc.ForceServerCodecV2(codec))
	s.Register(service, opts...)
}
