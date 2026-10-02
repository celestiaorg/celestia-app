package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestServerServeFailureIsReported checks that a listener failure closes Done and sets Err.
func TestServerServeFailureIsReported(t *testing.T) {
	srv, err := Listen("127.0.0.1:0", DefaultMaxConnections, DefaultMaxConcurrentStreams)
	require.NoError(t, err)
	srv.Register(nil)
	srv.Serve()

	require.NoError(t, srv.listener.Close())
	select {
	case <-srv.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done was not closed after the listener failed")
	}
	require.Error(t, srv.Err())
	srv.Stop(context.Background())
}
