package grpc

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
)

type heldRPCs struct {
	types.UnimplementedFibreServer
	entered chan struct{}
	release chan struct{}
}

func (s *heldRPCs) hold() {
	s.entered <- struct{}{}
	<-s.release // Deliberately retain work after client cancellation.
}

func (s *heldRPCs) DownloadShard(context.Context, *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	s.hold()
	return &types.DownloadShardResponse{}, nil
}

func (s *heldRPCs) UploadShard(context.Context, *types.UploadShardRequest) (*types.UploadShardResponse, error) {
	s.hold()
	return &types.UploadShardResponse{}, nil
}

func TestRPCAdmission(t *testing.T) {
	srv, err := Listen("127.0.0.1:0", 16, 32, 20, 8, false)
	require.NoError(t, err)
	service := &heldRPCs{entered: make(chan struct{}, 32), release: make(chan struct{})}
	srv.Register(service, grpc.ForceServerCodecV2(NewServerCodec(1721, 14)))
	srv.Serve()
	t.Cleanup(func() { close(service.release); srv.Stop(context.Background()) })
	dial := func() *grpc.ClientConn {
		cc, err := grpc.NewClient(srv.ListenAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = cc.Close() })
		return cc
	}
	cc := dial()
	client := types.NewFibreClient(cc)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := func(upload bool) {
		go func() {
			if upload {
				_, _ = client.UploadShard(ctx, &types.UploadShardRequest{})
			} else {
				_, _ = client.DownloadShard(ctx, &types.DownloadShardRequest{})
			}
		}()
		select {
		case <-service.entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	reject := func(upload bool, c types.FibreClient) {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		var err error
		if upload {
			_, err = c.UploadShard(ctx, &types.UploadShardRequest{})
		} else {
			_, err = c.DownloadShard(ctx, &types.DownloadShardRequest{})
		}
		require.Equal(t, codes.ResourceExhausted, status.Code(err))
	}
	for range 12 {
		start(false)
	}
	reject(false, client)
	for range 8 {
		start(true)
	}
	reject(true, client)
	stream, err := cc.NewStream(ctx, &grpc.StreamDesc{}, "/celestia.fibre.v1.Fibre/UploadShard")
	require.NoError(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(stream.RecvMsg(&types.UploadShardResponse{})), "reject before receiving any request body")
	// Closing the connection does not release slots held by unfinished handlers.
	require.NoError(t, cc.Close())
	other := types.NewFibreClient(dial())
	reject(false, other)
	reject(true, other)
}

func TestRPCResponseOwnership(t *testing.T) {
	for _, size := range []int{0, 1, 2 << 20} {
		a := &admission{limit: 1, readLimit: 1}
		slot, err := a.acquire(false)
		require.NoError(t, err)
		response := &types.DownloadShardResponse{}
		if size > 0 {
			response.Shard = &types.BlobShard{Rows: []*types.BlobRow{{Data: make([]byte, size)}}}
		}
		want, err := response.Marshal()
		require.NoError(t, err)
		wrapped := &rpcResponse{message: response, slot: slot}
		data, err := NewServerCodec(1721, 14).Marshal(wrapped)
		require.NoError(t, err)
		require.True(t, bytes.Equal(want, data.Materialize()))
		require.Nil(t, wrapped.message)
		if size == 0 {
			slot.release()
			require.Zero(t, a.total)
			continue
		}
		data.Ref()
		tail := data[0].Slice(1, data[0].Len())
		data.Free()
		slot.release()
		_, err = a.acquire(false)
		require.Equal(t, codes.ResourceExhausted, status.Code(err))
		data.Free()
		require.Equal(t, 1, a.total)
		tail.Free()
		require.Zero(t, a.total)
	}
}

func TestRPCAdmissionDisabled(t *testing.T) {
	srv, err := Listen("127.0.0.1:0", 2, 13, 1, 1, true)
	require.NoError(t, err)
	service := &heldRPCs{entered: make(chan struct{}, 6), release: make(chan struct{})}
	srv.Register(service)
	srv.Serve()
	t.Cleanup(func() { close(service.release); srv.Stop(context.Background()) })
	cc, err := grpc.NewClient(srv.ListenAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer cc.Close()
	client := types.NewFibreClient(cc)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for range 3 {
		go func() { _, _ = client.DownloadShard(ctx, &types.DownloadShardRequest{}) }()
		go func() { _, _ = client.UploadShard(ctx, &types.UploadShardRequest{}) }()
	}
	for range 6 {
		select {
		case <-service.entered:
		case <-ctx.Done():
			t.Fatal("disabled admission must allow reads and uploads beyond the configured limits")
		}
	}
}

func TestRPCUploadsUseSharedSlots(t *testing.T) {
	a := &admission{limit: 20, readLimit: 12}
	for range 20 {
		slot, err := a.acquire(true)
		require.NoError(t, err)
		defer slot.release()
	}
	_, err := a.acquire(true)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	_, err = a.acquire(false)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

type immediateRPCs struct{ types.UnimplementedFibreServer }

func (*immediateRPCs) DownloadShard(_ context.Context, req *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	if len(req.BlobId) == 1 {
		panic("test panic")
	}
	return &types.DownloadShardResponse{Shard: &types.BlobShard{Rlcs: make([]byte, 2048)}}, nil
}

type malformedCodec struct{ encoding.CodecV2 }

func (malformedCodec) Marshal(any) (mem.BufferSlice, error) {
	return mem.BufferSlice{mem.SliceBuffer{0xff}}, nil
}

func TestRPCFailuresAndCompression(t *testing.T) {
	srv, err := Listen("127.0.0.1:0", 2, 13, 1, 0, false)
	require.NoError(t, err)
	srv.Register(&immediateRPCs{}, grpc.MaxRecvMsgSize(1024))
	srv.Serve()
	t.Cleanup(func() { srv.Stop(context.Background()) })
	cc, err := grpc.NewClient(srv.ListenAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer cc.Close()
	client := types.NewFibreClient(cc)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for range 3 {
		for _, request := range []*types.DownloadShardRequest{{BlobId: []byte{1}}, {BlobId: make([]byte, 2048)}} {
			_, err := client.DownloadShard(ctx, request, grpc.UseCompressor(gzip.Name))
			require.Error(t, err)
			require.Eventually(t, func() bool { srv.admission.mu.Lock(); defer srv.admission.mu.Unlock(); return srv.admission.total == 0 }, time.Second, time.Millisecond)
		}
		_, err = client.UploadShard(ctx, &types.UploadShardRequest{}, grpc.ForceCodecV2(malformedCodec{NewServerCodec(1721, 14)}))
		require.Error(t, err)
		require.Eventually(t, func() bool { srv.admission.mu.Lock(); defer srv.admission.mu.Unlock(); return srv.admission.total == 0 }, time.Second, time.Millisecond)
		_, err = client.UploadShard(ctx, &types.UploadShardRequest{}, grpc.UseCompressor(gzip.Name))
		require.Equal(t, codes.Unimplemented, status.Code(err))
		response, err := client.DownloadShard(ctx, &types.DownloadShardRequest{}, grpc.UseCompressor(gzip.Name))
		require.NoError(t, err)
		require.Len(t, response.Shard.Rlcs, 2048)
		require.Eventually(t, func() bool { srv.admission.mu.Lock(); defer srv.admission.mu.Unlock(); return srv.admission.total == 0 }, time.Second, time.Millisecond)
	}
}
