package grpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestUploadDecodeReusesBacking(t *testing.T) {
	req := makeUploadShard(testMaxRows, testMaxProofs)
	req.Shard.Rows[0].Data = make([]byte, 2<<20)
	wire := marshalUploadShard(t, req)
	input := mem.BufferSlice{mem.SliceBuffer(wire)}
	codec := NewServerCodec(testMaxRows, testMaxProofs).(*pooledCodec)
	codec.uploads = &uploadBuffers{limit: len(wire)}
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			var got types.UploadShardRequest
			if err := codec.Unmarshal(input, &got); err != nil {
				b.Fatal(err)
			}
			codec.uploads.release(&got)
		}
	})
	require.Less(t, result.AllocedBytesPerOp(), int64(1<<20), "warmed decoding must not allocate a full payload")
}

func TestUploadBufferLifetime(t *testing.T) {
	for _, outcome := range []string{"success", "error", "cancel", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			req := makeUploadShard(testMaxRows, testMaxProofs)
			req.Shard.Rows[0].Data = bytes.Repeat([]byte{7}, minUploadBufferSize)
			wire := marshalUploadShard(t, req)
			codec := NewServerCodec(testMaxRows, testMaxProofs).(*pooledCodec)
			codec.uploads = &uploadBuffers{limit: 2 * len(wire)}
			input := mem.BufferSlice{mem.SliceBuffer(bytes.Clone(wire))}
			var got types.UploadShardRequest
			require.NoError(t, codec.Unmarshal(input, &got))
			clear(input[0].ReadOnlyData())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := codec.uploads.intercept(ctx, &got, nil, func(ctx context.Context, request any) (any, error) {
				require.Empty(t, codec.uploads.idle)
				scratch := codec.uploads.get(len(wire))
				for i := range scratch {
					scratch[i] = 0xa5
				}
				codec.uploads.put(scratch)
				require.Equal(t, req, request)
				return recoverUnaryInterceptor(ctx, request, &grpc.UnaryServerInfo{FullMethod: "test"}, func(ctx context.Context, _ any) (any, error) {
					switch outcome {
					case "error":
						return nil, errors.New("handler error")
					case "cancel":
						cancel()
						return nil, ctx.Err()
					case "panic":
						panic("handler panic")
					default:
						return &types.UploadShardResponse{}, nil
					}
				})
			})
			if outcome == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Empty(t, codec.uploads.active)
			require.Len(t, codec.uploads.idle, 2)
			codec.uploads.release(&got)
			require.Len(t, codec.uploads.idle, 2, "release must be idempotent")
		})
	}
}

func TestUploadReuseFallbackAndErrors(t *testing.T) {
	req := makeUploadShard(testMaxRows, testMaxProofs)
	req.Shard.Rows[0].Data = bytes.Repeat([]byte{7}, minUploadBufferSize)
	wire := marshalUploadShard(t, req)
	fallback := protowire.AppendTag(bytes.Clone(wire), 7, protowire.VarintType)
	fallback = protowire.AppendVarint(fallback, 1)
	tooMany := makeUploadShard(testMaxRows+1, 0)
	tooMany.Shard.Rlcs = make([]byte, minUploadBufferSize)
	for name, data := range map[string][]byte{
		"fallback":        fallback,
		"truncated":       wire[:len(wire)-1],
		"row limit":       marshalUploadShard(t, tooMany),
		"generated error": append(bytes.Clone(fallback), 0),
	} {
		t.Run(name, func(t *testing.T) {
			codec := NewServerCodec(testMaxRows, testMaxProofs).(*pooledCodec)
			codec.uploads = &uploadBuffers{limit: len(data)}
			var got types.UploadShardRequest
			err := codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(data)}, &got)
			if name == "fallback" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Empty(t, codec.uploads.active)
			require.Len(t, codec.uploads.idle, 1)
			for i := range codec.uploads.idle[0] {
				codec.uploads.idle[0][i] = 0xa5
			}
			if name == "fallback" {
				require.Equal(t, req, &got)
			}
		})
	}
}

func TestUploadBufferBoundAndOverwrite(t *testing.T) {
	pool := &uploadBuffers{limit: 3 * minUploadBufferSize}
	large := pool.get(2 * minUploadBufferSize)
	for i := range large {
		large[i] = 0xa5
	}
	pool.put(large)
	pool.put(make([]byte, 2*minUploadBufferSize))
	pool.put(make([]byte, minUploadBufferSize-1))
	require.Equal(t, 2*minUploadBufferSize, pool.idleBytes)
	require.Len(t, pool.idle, 1)
	reused := pool.get(minUploadBufferSize)
	require.Equal(t, byte(0xa5), reused[0], "cache must not clear reusable storage")
	require.Same(t, &large[0], &reused[0])
	pool.put(reused)
	req := makeUploadShard(1, 1)
	req.Shard.Rows[0].Data = make([]byte, minUploadBufferSize)
	wire := marshalUploadShard(t, req)
	codec := NewServerCodec(testMaxRows, testMaxProofs).(*pooledCodec)
	codec.uploads = pool
	var got types.UploadShardRequest
	require.NoError(t, codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(wire[:13]), mem.SliceBuffer(wire[13:])}, &got))
	require.Equal(t, req, &got)
	require.Equal(t, len(got.Shard.Rows[0].Data), cap(got.Shard.Rows[0].Data))
	codec.uploads.release(&got)
}

func TestUploadReuseConcurrentReaders(t *testing.T) {
	codec := NewServerCodec(testMaxRows, testMaxProofs).(*pooledCodec)
	codec.uploads = &uploadBuffers{limit: 4 * minUploadBufferSize}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			req := makeUploadShard(1, 1)
			req.Shard.Rows[0].Data = bytes.Repeat([]byte{byte(worker)}, minUploadBufferSize)
			wire, err := req.Marshal()
			if !assert.NoError(t, err) {
				return
			}
			for range 8 {
				var got types.UploadShardRequest
				if !assert.NoError(t, codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(wire)}, &got)) {
					return
				}
				_, err := codec.uploads.intercept(context.Background(), &got, nil, func(_ context.Context, _ any) (any, error) {
					// Model a synchronous store waiting for its body reader to finish.
					done := make(chan struct{})
					go func() { defer close(done); runtime.Gosched(); assert.Equal(t, req, &got) }()
					<-done
					return nil, nil
				})
				assert.NoError(t, err)
			}
		})
	}
	wg.Wait()
	require.Empty(t, codec.uploads.active)
	require.LessOrEqual(t, codec.uploads.idleBytes, codec.uploads.limit)
}

type reuseTestService struct {
	types.UnimplementedFibreServer
	upload func(*types.UploadShardRequest)
}

func (s *reuseTestService) UploadShard(_ context.Context, req *types.UploadShardRequest) (*types.UploadShardResponse, error) {
	s.upload(req)
	return &types.UploadShardResponse{}, nil
}

func TestUploadReuseGRPC(t *testing.T) {
	for _, tracing := range []bool{false, true} {
		t.Run(fmt.Sprint("tracing=", tracing), func(t *testing.T) {
			before := grpc.EnableTracing
			grpc.EnableTracing = tracing
			defer func() { grpc.EnableTracing = before }()
			server, err := Listen("127.0.0.1:0", 2, 2)
			require.NoError(t, err)
			defer server.Stop(context.Background())
			var previous *types.UploadShardRequest
			calls := 0
			service := &reuseTestService{upload: func(req *types.UploadShardRequest) {
				calls++
				if calls == 1 {
					previous = req
					return
				}
				if tracing {
					assert.Equal(t, byte(7), previous.Shard.Rows[0].Data[0])
				} else {
					assert.Same(t, &previous.Shard.Rows[0].Data[0], &req.Shard.Rows[0].Data[0])
				}
			}}
			server.RegisterWithUploadBufferReuse(service, 4<<20, testMaxRows, testMaxProofs,
				grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
					upload := req.(*types.UploadShardRequest)
					first := upload.Shard.Rows[0].Data[0]
					resp, err := next(ctx, req)
					assert.Equal(t, first, upload.Shard.Rows[0].Data[0], "interceptors must retain request access")
					return resp, err
				}))
			server.Serve()
			conn, err := grpc.NewClient(server.ListenAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.CallContentSubtype(codecName)))
			require.NoError(t, err)
			defer conn.Close()
			client := types.NewFibreClient(conn)
			req := makeUploadShard(1, 1)
			req.Shard.Rows[0].Data = bytes.Repeat([]byte{7}, minUploadBufferSize)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err = client.UploadShard(ctx, req)
			require.NoError(t, err)
			req.Shard.Rows[0].Data[0] = 9
			_, err = client.UploadShard(ctx, req)
			require.NoError(t, err)
		})
	}
}
