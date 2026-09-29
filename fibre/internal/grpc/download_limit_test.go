package grpc

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestDownloadLimit(t *testing.T) {
	const limit = 4
	entered := make(chan struct{}, limit+1)
	unblock := make(chan struct{})
	service := &blockedDownloads{entered: entered, unblock: unblock}
	srv, err := Listen("127.0.0.1:0", 8, 8, limit)
	require.NoError(t, err)
	srv.Register(service, grpc.ForceServerCodecV2(NewServerCodec(4096, 14)))
	srv.Serve()
	t.Cleanup(func() { close(unblock); srv.Stop(t.Context()) })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	dial := func() *grpc.ClientConn {
		cc, err := grpc.NewClient(srv.ListenAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.CallContentSubtype(codecName)))
		require.NoError(t, err)
		t.Cleanup(func() { _ = cc.Close() })
		return cc
	}
	client := types.NewFibreClient(dial())
	for _, code := range []codes.Code{codes.NotFound, codes.Internal} {
		for range limit + 1 {
			_, err := client.DownloadShard(ctx, &types.DownloadShardRequest{BlobId: []byte{byte(code)}})
			require.Equal(t, code, status.Code(err), "failed downloads must release their slots")
		}
	}
	for range limit {
		go func() { _, _ = client.DownloadShard(ctx, &types.DownloadShardRequest{}) }()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	_, err = client.UploadShard(ctx, &types.UploadShardRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err), "downloads must not occupy upload slots")
	// A separate connection must share the same limit.
	other := types.NewFibreClient(dial())
	result := make(chan error, 1)
	go func() { _, err := other.DownloadShard(ctx, &types.DownloadShardRequest{}); result <- err }()
	select {
	case <-entered:
		t.Fatal("excess download reached the payload reader")
	case err := <-result:
		require.Equal(t, codes.ResourceExhausted, status.Code(err))
		require.IsType(t, &errdetails.RetryInfo{}, status.Convert(err).Details()[0])
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

type blockedDownloads struct {
	types.UnimplementedFibreServer
	entered chan struct{}
	unblock chan struct{}
}

func (s *blockedDownloads) DownloadShard(ctx context.Context, req *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	if len(req.BlobId) > 0 {
		if codes.Code(req.BlobId[0]) == codes.Internal {
			panic("handler failure")
		}
		return nil, status.Error(codes.NotFound, "missing shard")
	}
	s.entered <- struct{}{}
	select {
	case <-s.unblock:
		return &types.DownloadShardResponse{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestDownloadResponseLifetime(t *testing.T) {
	for _, size := range []int{0, 1, 2 << 20} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			response := &types.DownloadShardResponse{}
			if size > 0 {
				response.Shard = &types.BlobShard{Rows: []*types.BlobRow{{Data: make([]byte, size)}}}
			}
			want, err := response.Marshal()
			require.NoError(t, err)
			released := 0
			wrapped := &downloadResponse{DownloadShardResponse: response, release: func() { released++ }}
			codec := NewServerCodec(4096, 14)
			data, err := codec.Marshal(wrapped)
			require.NoError(t, err)
			require.Nil(t, wrapped.DownloadShardResponse)
			require.True(t, bytes.Equal(want, data.Materialize()), "wire bytes must match")
			if size == 0 {
				require.Equal(t, 1, released)
				return
			}
			data.Ref()
			tail := data[0].Slice(1, data[0].Len())
			data.Free()
			require.Zero(t, released)
			data.Free()
			require.Zero(t, released)
			tail.Free()
			require.Equal(t, 1, released)
		})
	}
}
