package grpc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// stallingServer blocks the first DownloadShard until its stream ends and
// answers later calls immediately.
type stallingServer struct {
	types.UnimplementedFibreServer
	calls   atomic.Int32
	stalled chan struct{}
}

func (s *stallingServer) DownloadShard(ctx context.Context, _ *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	if s.calls.Add(1) == 1 {
		close(s.stalled)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &types.DownloadShardResponse{}, nil
}

// TestMaxConnectionAgeFreesStalledSlot checks that a connection held by an
// unfinished RPC is closed after age and grace, freeing its slot.
func TestMaxConnectionAgeFreesStalledSlot(t *testing.T) {
	origAge, origGrace := keepAliveMaxConnAge, keepAliveMaxConnAgeGrace
	keepAliveMaxConnAge, keepAliveMaxConnAgeGrace = 200*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { keepAliveMaxConnAge, keepAliveMaxConnAgeGrace = origAge, origGrace })

	srv, err := Listen("127.0.0.1:0", 1, DefaultMaxConcurrentStreams, 20, 8, false)
	require.NoError(t, err)
	service := &stallingServer{stalled: make(chan struct{})}
	srv.Register(service)
	srv.Serve()
	t.Cleanup(func() { srv.Stop(context.Background()) })

	dial := func() types.FibreClient {
		conn, err := grpc.NewClient(srv.ListenAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		return types.NewFibreClient(conn)
	}

	// Hold the only slot with an RPC that never finishes.
	stalledErr := make(chan error, 1)
	go func() {
		_, err := dial().DownloadShard(context.Background(), &types.DownloadShardRequest{})
		stalledErr <- err
	}()
	select {
	case <-service.stalled:
	case err := <-stalledErr:
		t.Fatalf("stalled RPC ended before reaching the handler: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("stalled RPC never reached the handler")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = dial().DownloadShard(ctx, &types.DownloadShardRequest{}, grpc.WaitForReady(true))
	require.NoError(t, err)

	select {
	case err := <-stalledErr:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("stalled RPC was not closed")
	}
}
