package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// fakeFibre signals handler entry on entered (if set) and blocks until release
// is closed (if set) or, unless ignoreCancel, the request ends. Uploads panic
// when panics is set.
type fakeFibre struct {
	types.UnimplementedFibreServer
	entered      chan struct{}
	release      chan struct{}
	ignoreCancel bool
	panics       bool
}

func (f *fakeFibre) wait(ctx context.Context) error {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release == nil {
		return nil
	}
	if f.ignoreCancel { // like a store read that outlives the request
		<-f.release
		return nil
	}
	select {
	case <-f.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeFibre) UploadShard(ctx context.Context, _ *types.UploadShardRequest) (*types.UploadShardResponse, error) {
	if f.panics {
		panic("boom")
	}
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	return &types.UploadShardResponse{ValidatorSignature: []byte("ok")}, nil
}

func (f *fakeFibre) DownloadShard(ctx context.Context, _ *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	return &types.DownloadShardResponse{}, nil
}

// startServer serves svc and gRPC health with admission cfg and returns a client conn.
func startServer(t *testing.T, cfg AdmissionConfig, svc types.FibreServer) (*Server, *grpc.ClientConn) {
	t.Helper()
	srv, err := Listen("127.0.0.1:0", DefaultMaxConnections, 100, cfg)
	require.NoError(t, err)
	srv.Register(svc)
	healthpb.RegisterHealthServer(srv, health.NewServer())
	srv.Serve()
	t.Cleanup(func() { srv.Stop(context.Background()) })

	conn, err := grpc.NewClient(srv.ListenAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return srv, conn
}

func uploadReq(payload int) *types.UploadShardRequest {
	return &types.UploadShardRequest{Shard: &types.BlobShard{Rlcs: make([]byte, payload)}}
}

// TestAdmissionServesMethods: the generated unary clients work against the
// admission-wrapped handlers, and health Watch stays unsupported.
func TestAdmissionServesMethods(t *testing.T) {
	_, conn := startServer(t, testAdmission, &fakeFibre{})
	c := types.NewFibreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.UploadShard(ctx, &types.UploadShardRequest{})
	require.NoError(t, err)
	require.Equal(t, []byte("ok"), resp.ValidatorSignature)
	_, err = c.DownloadShard(ctx, &types.DownloadShardRequest{})
	require.NoError(t, err)

	hc := healthpb.NewHealthClient(conn)
	_, err = hc.Check(ctx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	stream, err := hc.Watch(ctx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.Equal(t, grpccodes.Unimplemented, status.Code(err))
}

// TestAdmissionGatesAllMethods: with the budget held by a blocked upload, every
// method is rejected before its body is read, and the rejection names the method.
func TestAdmissionGatesAllMethods(t *testing.T) {
	withShortWait(t)
	const msg = 1 << 16
	log := &rejectLog{}
	svc := &fakeFibre{entered: make(chan struct{}, 1), release: make(chan struct{})}
	_, conn := startServer(t, AdmissionConfig{Budget: msg, MaxMessage: msg, OnReject: log.onReject}, svc)
	c := types.NewFibreClient(conn)
	defer close(svc.release)

	go func() { _, _ = c.UploadShard(context.Background(), uploadReq(msg/2)) }()
	<-svc.entered // holds the budget

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.UploadShard(ctx, &types.UploadShardRequest{})
	requireRetryable(t, err)
	_, err = c.DownloadShard(ctx, &types.DownloadShardRequest{})
	requireRetryable(t, err)
	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	requireRetryable(t, err)
	require.Equal(t, []string{"UploadShard/memory_timeout", "DownloadShard/memory_timeout", "Check/memory_timeout"}, log.reasons())
}

// TestAdmissionShrinkAdmitsNext: a small upload shrinks its reservation, freeing
// budget for a second upload that would not fit at the max reservation.
func TestAdmissionShrinkAdmitsNext(t *testing.T) {
	const msg = 1 << 16
	svc := &fakeFibre{entered: make(chan struct{}, 2), release: make(chan struct{})}
	_, conn := startServer(t, AdmissionConfig{Budget: msg + msg/2, MaxMessage: msg}, svc) // fits one max, not two
	c := types.NewFibreClient(conn)
	defer close(svc.release)

	for range 2 {
		go func() { _, _ = c.UploadShard(context.Background(), &types.UploadShardRequest{}) }()
		select {
		case <-svc.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("upload not admitted after the previous one shrank")
		}
	}
}

// TestAdmissionDeadlineReleasesStalledSender: a client that opens a stream but
// never sends has its reservation released when the request deadline fires.
func TestAdmissionDeadlineReleasesStalledSender(t *testing.T) {
	orig := requestDeadline
	requestDeadline = 500 * time.Millisecond
	t.Cleanup(func() { requestDeadline = orig })

	const msg = 1 << 16
	srv, conn := startServer(t, AdmissionConfig{Budget: msg, MaxMessage: msg}, &fakeFibre{})
	desc := &grpc.StreamDesc{StreamName: "UploadShard", ClientStreams: true, ServerStreams: true}
	_, err := conn.NewStream(t.Context(), desc, "/celestia.fibre.v1.Fibre/UploadShard")
	require.NoError(t, err)

	require.Eventually(t, func() bool { return srv.adm.reserved.Load() == msg }, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return srv.adm.reserved.Load() == 0 }, 3*time.Second, 10*time.Millisecond)
}

// TestAdmissionRecoversPanic: a handler panic becomes Internal and releases the
// reservation instead of crashing the process.
func TestAdmissionRecoversPanic(t *testing.T) {
	srv, conn := startServer(t, testAdmission, &fakeFibre{panics: true})
	_, err := types.NewFibreClient(conn).UploadShard(t.Context(), &types.UploadShardRequest{})
	require.Equal(t, grpccodes.Internal, status.Code(err))
	require.Eventually(t, func() bool { return srv.adm.reserved.Load() == 0 }, 2*time.Second, 10*time.Millisecond)
}

// TestDownloadHoldsMaxUntilReturn: a download keeps its max reservation while the
// handler runs, so a second download is rejected, and a client reset (RST_STREAM
// cancels the stream context) does not release it before the handler returns.
func TestDownloadHoldsMaxUntilReturn(t *testing.T) {
	withShortWait(t)
	const msg = 1 << 16
	svc := &fakeFibre{entered: make(chan struct{}, 1), release: make(chan struct{}), ignoreCancel: true}
	srv, conn := startServer(t, AdmissionConfig{Budget: msg, MaxMessage: msg}, svc) // budget fits one
	c := types.NewFibreClient(conn)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.DownloadShard(ctx, &types.DownloadShardRequest{}); done <- err }()
	<-svc.entered
	require.EqualValues(t, msg, srv.adm.reserved.Load())

	_, err := c.DownloadShard(t.Context(), &types.DownloadShardRequest{})
	requireRetryable(t, err)

	cancel()
	require.Equal(t, grpccodes.Canceled, status.Code(<-done))
	require.Never(t, func() bool { return srv.adm.reserved.Load() < msg }, 300*time.Millisecond, 10*time.Millisecond)

	close(svc.release)
	require.Eventually(t, func() bool { return srv.adm.reserved.Load() == 0 }, 2*time.Second, 10*time.Millisecond)
}
