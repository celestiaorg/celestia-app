package grpc

import (
	"context"
	"crypto/tls"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/internal/tlsid"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type admissionService struct {
	types.UnimplementedFibreServer
	entered chan struct{}
}

func (*admissionService) UploadShard(_ context.Context, req *types.UploadShardRequest) (*types.UploadShardResponse, error) {
	if req.Shard == nil {
		panic("test recovery")
	}
	return &types.UploadShardResponse{ValidatorSignature: []byte{byte(len(req.Shard.Rows))}}, nil
}

func (s *admissionService) DownloadShard(ctx context.Context, req *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	if len(req.BlobId) == 2 {
		close(s.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &types.DownloadShardResponse{}, nil
}

func TestMemoryAdmissionTLS(t *testing.T) {
	previous := receiveTimeout
	receiveTimeout = time.Second
	t.Cleanup(func() { receiveTimeout = previous })
	pv := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(pv, "admission-test")
	require.NoError(t, err)
	pub, err := pv.GetPubKey()
	require.NoError(t, err)
	a := NewAdmission(128<<20, 8<<20, 4096, 14)
	a.DownloadSize = func(_ context.Context, id []byte) (int64, error) {
		if len(id) == 1 {
			return 8 << 20, nil
		}
		return 1024, nil
	}

	srv, err := Listen("127.0.0.1:0", 2, 13)
	require.NoError(t, err)
	entered := make(chan struct{})
	var intercepted atomic.Int32
	interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		intercepted.Add(1)
		if download, ok := req.(*types.DownloadShardRequest); ok && len(download.BlobId) >= 3 {
			if len(download.BlobId) == 3 {
				if err := grpc.SetSendCompressor(ctx, gzip.Name); err != nil {
					return nil, err
				}
			}
			if err := grpc.SendHeader(ctx, metadata.Pairs("test", "early-header")); err != nil {
				return nil, err
			}
		}
		return handler(ctx, req)
	}
	srv.Register(&admissionService{entered: entered}, a, credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}), interceptor)
	srv.Serve()
	t.Cleanup(func() { srv.Stop(context.Background()) })
	conn, err := grpc.NewClient(srv.ListenAddress(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, //nolint:gosec // Authenticate the validator endorsement below.
		VerifyConnection:   tlsid.VerifyConnection(pub, "admission-test"),
	})), grpc.WithDefaultCallOptions(grpc.CallContentSubtype(codecName)))
	require.NoError(t, err)
	defer conn.Close()
	client := types.NewFibreClient(conn)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	request := &types.UploadShardRequest{Shard: &types.BlobShard{}}
	for range 64 {
		request.Shard.Rows = append(request.Shard.Rows, &types.BlobRow{Data: make([]byte, 32<<10)})
	}
	for _, compression := range []string{"identity", gzip.Name} {
		response, err := client.UploadShard(ctx, request, grpc.UseCompressor(compression))
		require.NoError(t, err)
		require.Equal(t, []byte{64}, response.ValidatorSignature)
		require.Positive(t, intercepted.Load(), "configured unary interceptors must run")
	}
	busy := newMemoryLease(a.memoryBudget, false)
	require.NoError(t, busy.reserve(ctx, 96<<20))
	_, err = client.DownloadShard(ctx, &types.DownloadShardRequest{BlobId: []byte{1}})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	busy.release()
	_, err = client.UploadShard(ctx, &types.UploadShardRequest{})
	require.Equal(t, codes.Internal, status.Code(err))
	_, err = client.DownloadShard(ctx, &types.DownloadShardRequest{})
	require.NoError(t, err)
	require.EqualValues(t, 4, intercepted.Load(), "rejected downloads must not enter the interceptor or handler")
	_, err = client.DownloadShard(ctx, &types.DownloadShardRequest{BlobId: []byte{1, 2, 3}})
	require.Equal(t, codes.Internal, status.Code(err), "compressed headers must not bypass response lease tracking")
	_, err = client.DownloadShard(ctx, &types.DownloadShardRequest{BlobId: []byte{1, 2, 3, 4}})
	require.NoError(t, err, "early identity headers remain supported")
	cancelCtx, stop := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() {
		_, err := client.DownloadShard(cancelCtx, &types.DownloadShardRequest{BlobId: []byte{1, 2}})
		finished <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-finished:
		t.Fatalf("receive timer cancelled a running handler: %v", err)
	case <-time.After(2 * receiveTimeout):
	}
	stop()
	require.Equal(t, codes.Canceled, status.Code(<-finished))
	require.Eventually(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.used == 0 && a.downloads == 0
	}, time.Second, time.Millisecond)
}
