package grpc

import (
	"context"
	"fmt"
	"net"

	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	"google.golang.org/grpc"
)

// Server wraps a [grpc.Server] with TCP listener and lifecycle management.
type Server struct {
	server   *grpc.Server
	listener net.Listener
	done     chan struct{}
}

// NewServer creates a Fibre gRPC [Server] that listens on the given address
// and registers the provided [types.FibreServer] service.
func NewServer(listenAddr string, service types.FibreServer, opts ...grpc.ServerOption) (*Server, error) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", listenAddr, err)
	}
	server := grpc.NewServer(opts...)
	types.RegisterFibreServer(server, service)
	return &Server{
		server:   server,
		listener: listener,
	}, nil
}

// ListenAddress returns the actual address the server is listening on.
func (s *Server) ListenAddress() string {
	return s.listener.Addr().String()
}

// Serve starts serving gRPC requests in a background goroutine.
func (s *Server) Serve() {
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		_ = s.server.Serve(s.listener)
	}()
}

// Stop gracefully stops the gRPC server.
// If the context is cancelled before draining completes, it forces an immediate stop.
func (s *Server) Stop(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		s.server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		s.server.Stop()
	}

	<-s.done
}
