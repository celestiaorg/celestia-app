package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// blockingDownload blocks DownloadShard so a test can hold its reservation.
type blockingDownload struct {
	types.UnimplementedFibreServer
	entered chan struct{}
	release chan struct{}
}

func (f *blockingDownload) DownloadShard(ctx context.Context, _ *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
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
	return &types.DownloadShardResponse{}, nil
}

// TestDownloadReservesResponse: a download keeps its (max) reservation through the
// handler, so while one is in flight the budget is full and the next is rejected.
// (In the admission-only change the download would shrink to its tiny request and
// the second would fit.)
func TestDownloadReservesResponse(t *testing.T) {
	orig := admissionWait
	admissionWait = 100 * time.Millisecond
	t.Cleanup(func() { admissionWait = orig })

	const msg = 1 << 16
	svc := &blockingDownload{entered: make(chan struct{}, 1), release: make(chan struct{})}
	srv := startAdmission(t, msg, msg, svc) // budget fits one
	c := dial(t, srv.ListenAddress())
	defer close(svc.release)

	go func() { _, _ = c.DownloadShard(context.Background(), &types.DownloadShardRequest{}) }()
	<-svc.entered // A holds the budget in the handler

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.DownloadShard(ctx, &types.DownloadShardRequest{})
	require.Equal(t, grpccodes.ResourceExhausted, status.Code(err))
}

// TestDownloadReleasesAtStreamEnd: a completed download releases its reservation.
func TestDownloadReleasesAtStreamEnd(t *testing.T) {
	const msg = 1 << 16
	srv := startAdmission(t, msg, msg, &fakeFibre{})
	c := dial(t, srv.ListenAddress())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.DownloadShard(ctx, &types.DownloadShardRequest{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return srv.adm.reserved.Load() == 0 }, 2*time.Second, 10*time.Millisecond)
}
