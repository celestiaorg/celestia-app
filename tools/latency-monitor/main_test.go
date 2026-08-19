package main

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

// TestTokenCredsAttachesHeader verifies that a connection dialed with
// tokenCreds sends the x-token header on every RPC.
func TestTokenCredsAttachesHeader(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var gotToken string
	srv := grpc.NewServer(grpc.UnaryInterceptor(
		func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			if md, ok := metadata.FromIncomingContext(ctx); ok {
				if v := md.Get("x-token"); len(v) > 0 {
					gotToken = v[0]
				}
			}
			return handler(ctx, req)
		},
	))
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(tokenCreds("secret-token")),
	)
	require.NoError(t, err)
	defer conn.Close()

	_, err = healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	require.Equal(t, "secret-token", gotToken)
}
