package grpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/stretchr/testify/require"
)

// TestServer_StopReleasesListenerWhenRegisteredButNotServed ensures that a
// startup that registers the gRPC server but fails before Serve (e.g. the store
// fails to open) does not leak the TCP listener. Previously GracefulStop did not
// close a listener grpc-go never saw, leaking the fd/port.
func TestServer_StopReleasesListenerWhenRegisteredButNotServed(t *testing.T) {
	srv, err := fibregrpc.Listen("127.0.0.1:0")
	require.NoError(t, err)

	addr := srv.ListenAddress()
	srv.Register(nil) // server created, but Serve is never called

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv.Stop(ctx)

	// The port must be free again immediately after Stop.
	ln, err := net.Listen("tcp", addr)
	require.NoError(t, err, "Stop should have closed the listener and freed the port")
	require.NoError(t, ln.Close())
}
