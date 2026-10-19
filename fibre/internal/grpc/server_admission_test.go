package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// fakeFibre records handler entry and optionally blocks uploads to hold budget.
type fakeFibre struct {
	types.UnimplementedFibreServer
	entered chan struct{}
	release chan struct{}
}

func (f *fakeFibre) UploadShard(ctx context.Context, _ *types.UploadShardRequest) (*types.UploadShardResponse, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &types.UploadShardResponse{ValidatorSignature: []byte("ok")}, nil
}

func (f *fakeFibre) DownloadShard(context.Context, *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	return &types.DownloadShardResponse{}, nil
}

func startAdmission(t *testing.T, budget, msg int64, svc types.FibreServer) *Server {
	t.Helper()
	srv, err := Listen("127.0.0.1:0", 0, 100) // per-IP disabled
	require.NoError(t, err)
	srv.Register(svc, AdmissionConfig{Budget: budget, MaxMessage: msg})
	srv.Serve()
	t.Cleanup(func() { srv.Stop(context.Background()) })
	return srv
}

func dial(t *testing.T, addr string) types.FibreClient {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return types.NewFibreClient(conn)
}

func uploadReq(payload int) *types.UploadShardRequest {
	return &types.UploadShardRequest{Shard: &types.BlobShard{Rlcs: make([]byte, payload)}}
}

// TestAdmissionUnaryClientWorks: the generated unary client works against the
// admission-wrapped stream handlers, for both upload and download.
func TestAdmissionUnaryClientWorks(t *testing.T) {
	srv := startAdmission(t, 1<<20, 1<<16, &fakeFibre{})
	c := dial(t, srv.ListenAddress())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.UploadShard(ctx, &types.UploadShardRequest{})
	require.NoError(t, err)
	require.Equal(t, []byte("ok"), resp.ValidatorSignature)

	_, err = c.DownloadShard(ctx, &types.DownloadShardRequest{})
	require.NoError(t, err)
}

// TestAdmissionGatesAllMethods: with the budget held by a blocked upload, both a
// second upload and a download are rejected with ResourceExhausted + RetryInfo —
// so every method (not just upload) is gated before RecvMsg.
func TestAdmissionGatesAllMethods(t *testing.T) {
	orig := admissionWait
	admissionWait = 100 * time.Millisecond
	t.Cleanup(func() { admissionWait = orig })

	const msg = 1 << 16
	svc := &fakeFibre{entered: make(chan struct{}, 1), release: make(chan struct{})}
	srv := startAdmission(t, msg, msg, svc) // budget fits one
	c := dial(t, srv.ListenAddress())
	defer close(svc.release)

	go func() { _, _ = c.UploadShard(context.Background(), uploadReq(msg/2)) }()
	<-svc.entered // A holds the budget

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, call := range []func() error{
		func() error { _, err := c.UploadShard(ctx, &types.UploadShardRequest{}); return err },
		func() error { _, err := c.DownloadShard(ctx, &types.DownloadShardRequest{}); return err },
	} {
		err := call()
		require.Equal(t, grpccodes.ResourceExhausted, status.Code(err))
		require.Positive(t, retryDelay(t, err))
	}
}

// TestAdmissionShrinkAdmitsNext: a small upload shrinks its reservation, freeing
// budget for a second upload that would not fit at the max reservation.
func TestAdmissionShrinkAdmitsNext(t *testing.T) {
	const msg = 1 << 16
	svc := &fakeFibre{entered: make(chan struct{}, 2), release: make(chan struct{})}
	srv := startAdmission(t, msg+msg/2, msg, svc) // fits one max, not two
	c := dial(t, srv.ListenAddress())
	defer close(svc.release)

	go func() { _, _ = c.UploadShard(context.Background(), &types.UploadShardRequest{}) }() // tiny -> shrinks
	<-svc.entered
	go func() { _, _ = c.UploadShard(context.Background(), &types.UploadShardRequest{}) }()
	select {
	case <-svc.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("second upload not admitted after the first shrank")
	}
}

// TestAdmissionDeadlineReleasesStalledSender: a client that opens a stream but
// never sends has its reservation released when the request deadline fires.
func TestAdmissionDeadlineReleasesStalledSender(t *testing.T) {
	orig := requestDeadline
	requestDeadline = 500 * time.Millisecond
	t.Cleanup(func() { requestDeadline = orig })

	const msg = 1 << 16
	srv := startAdmission(t, msg, msg, &fakeFibre{})
	conn, err := grpc.NewClient(srv.ListenAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	desc := &grpc.StreamDesc{StreamName: "UploadShard", ClientStreams: true, ServerStreams: true}
	_, err = conn.NewStream(ctx, desc, "/celestia.fibre.v1.Fibre/UploadShard")
	require.NoError(t, err)

	require.Eventually(t, func() bool { return srv.adm.reserved.Load() == msg }, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return srv.adm.reserved.Load() == 0 }, 3*time.Second, 10*time.Millisecond)
}

func retryDelay(t *testing.T, err error) time.Duration {
	t.Helper()
	st, _ := status.FromError(err)
	for _, d := range st.Details() {
		if ri, ok := d.(*errdetails.RetryInfo); ok {
			return ri.GetRetryDelay().AsDuration()
		}
	}
	return 0
}
