package abci

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// TestServeGRPCLimitsConnections verifies that the app gRPC listener serves at
// most maxConns connections at once and admits a waiting connection when a
// served one closes.
func TestServeGRPCLimitsConnections(t *testing.T) {
	const (
		maxConns = 2
		extra    = 3
	)

	lis, err := listenGRPC("127.0.0.1:0", maxConns)
	require.NoError(t, err)

	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, health.NewServer())

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- serveGRPC(ctx, log.NewNopLogger(), lis, srv) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-errCh)
	})

	// Each grpc.ClientConn uses its own TCP connection.
	clients := make([]*grpc.ClientConn, maxConns+extra)
	for i := range clients {
		cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = cc.Close() })
		clients[i] = cc
	}

	check := func(cc *grpc.ClientConn, timeout time.Duration) error {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		_, err := healthpb.NewHealthClient(cc).Check(ctx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
		return err
	}

	// The first maxConns clients are served.
	for i := range maxConns {
		require.NoError(t, check(clients[i], 5*time.Second), "client %d", i)
	}
	// The remaining clients are not served while all slots are in use.
	for i := maxConns; i < len(clients); i++ {
		require.Error(t, check(clients[i], 300*time.Millisecond), "client %d", i)
	}

	// Closing a served client frees a slot for the oldest waiting client.
	require.NoError(t, clients[0].Close())
	require.NoError(t, check(clients[maxConns], 10*time.Second))

	// The cap still holds for the other waiting clients.
	for i := maxConns + 1; i < len(clients); i++ {
		require.Error(t, check(clients[i], 300*time.Millisecond), "client %d", i)
	}
}
