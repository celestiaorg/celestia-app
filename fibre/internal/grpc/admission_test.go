package grpc

import (
	"context"
	"crypto/tls"
	"sync"
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
	"google.golang.org/grpc/status"
)

func testLease(a *Admission, download bool) *memoryLease {
	l := &memoryLease{budget: a, download: download}
	l.refs.Store(1)
	return l
}

func TestMemoryAdmission(t *testing.T) {
	a := NewAdmission(100, 60, 8<<20, 4096, 14)
	download, upload := testLease(a, true), testLease(a, false)
	require.NoError(t, download.reserve(40))
	require.Equal(t, codes.ResourceExhausted, status.Code(download.reserve(1)))
	require.NoError(t, upload.reserve(60))
	require.Equal(t, codes.ResourceExhausted, status.Code(upload.reserve(1)))
	download.release()
	require.NoError(t, upload.reserve(40), "uploads can use the entire budget")
	upload.release()
	require.Zero(t, a.used)
	require.Zero(t, a.downloads)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			l := testLease(a, true)
			_ = l.reserve(10)
			l.release()
		})
	}
	wg.Wait()
	require.Zero(t, a.used)
	disabled := NewAdmission(0, 60, 8<<20, 4096, 14)
	l := testLease(disabled, true)
	require.NoError(t, l.reserve(1000))
	l.release()
}

func TestMemoryResponseLifetime(t *testing.T) {
	a := NewAdmission(100, 0, 8<<20, 4096, 14)
	l := testLease(a, true)
	require.NoError(t, l.reserve(100))
	codec := &pooledCodec{pool: l}
	data, err := codec.Marshal(&types.UploadShardResponse{ValidatorSignature: []byte{1}})
	require.NoError(t, err)
	data.Ref()
	l.release()
	data.Free()
	require.EqualValues(t, 100, a.used)
	data.Free()
	require.Zero(t, a.used)
}

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
		if err := ReserveMemory(ctx, 1<<20); err != nil {
			return nil, err
		}
		close(s.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if len(req.BlobId) == 1 {
		if err := ReserveMemory(ctx, 128<<20); err != nil {
			return nil, err
		}
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
	a := NewAdmission(128<<20, 64<<20, 8<<20, 4096, 14)
	srv, err := Listen("127.0.0.1:0", 2, 13)
	require.NoError(t, err)
	entered := make(chan struct{})
	srv.Register(&admissionService{entered: entered}, a, grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})))
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
	}
	_, err = client.DownloadShard(ctx, &types.DownloadShardRequest{BlobId: []byte{1}})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	_, err = client.UploadShard(ctx, &types.UploadShardRequest{})
	require.Equal(t, codes.Internal, status.Code(err))
	_, err = client.DownloadShard(ctx, &types.DownloadShardRequest{})
	require.NoError(t, err)
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
