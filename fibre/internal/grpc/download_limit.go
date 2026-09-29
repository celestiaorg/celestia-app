package grpc

import (
	"context"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func downloadLimiter(limit int) grpc.UnaryServerInterceptor {
	slots := make(chan struct{}, limit)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info.FullMethod != "/celestia.fibre.v1.Fibre/DownloadShard" {
			return handler(ctx, req)
		}
		select {
		case slots <- struct{}{}:
		default:
			st := status.New(codes.ResourceExhausted, "fibre download limit reached")
			st, _ = st.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(time.Second)})
			return nil, st.Err()
		}
		release := sync.OnceFunc(func() { <-slots })
		transferred := false
		defer func() {
			if !transferred {
				release()
			}
		}()
		// Compression replaces the tracked buffer and would release its slot too early.
		if err := grpc.SetSendCompressor(ctx, encoding.Identity); err != nil {
			return nil, err
		}
		resp, err := handler(ctx, req)
		if err != nil {
			return nil, err
		}
		shard, ok := resp.(*types.DownloadShardResponse)
		if !ok || shard == nil {
			return nil, status.Error(codes.Internal, "invalid download response")
		}
		transferred = true
		return &downloadResponse{DownloadShardResponse: shard, release: release}, nil
	}
}

// downloadResponse transfers its slot to the transport-owned response buffer.
type downloadResponse struct {
	*types.DownloadShardResponse
	release func()
}

func (r *downloadResponse) marshal() (out mem.BufferSlice, err error) {
	defer func() {
		// Drop decoded payloads before the transport can release the slot.
		r.DownloadShardResponse = nil
		if len(out) == 0 {
			r.release()
		}
	}()
	size := r.Size()
	if size == 0 {
		return mem.BufferSlice{}, nil
	}
	capacity := max(size, 1)
	// Small buffers bypass mem.BufferPool.Put; force reference tracking for them too.
	for mem.IsBelowBufferPoolingThreshold(capacity) {
		capacity *= 2
	}
	pool := &downloadBufferPool{release: r.release}
	buf := pool.Get(capacity)
	*buf = (*buf)[:size]
	if _, err := r.MarshalToSizedBuffer(*buf); err != nil {
		return nil, err
	}
	return mem.BufferSlice{mem.NewBuffer(buf, pool)}, nil
}

// Download buffers are not cached, so completed responses cannot accumulate in a pool.
type downloadBufferPool struct {
	mem.NopBufferPool
	release func()
}

func (p *downloadBufferPool) Put(buf *[]byte) {
	*buf = nil
	p.release()
}
