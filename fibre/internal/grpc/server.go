package grpc

import (
	"context"
	"fmt"
	"net"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"google.golang.org/grpc"
)

// Server wraps a [grpc.Server] with TCP listener and lifecycle management.
type Server struct {
	server   *grpc.Server
	listener net.Listener
	done     chan struct{}
}

// Listen creates a [Server] bound to listenAddr. The underlying [grpc.Server]
// is created lazily by [Server.Register] so callers can defer building
// credentials until after the listener address is known (e.g., for TLS certs
// that depend on a chain ID resolved at startup).
func Listen(listenAddr string) (*Server, error) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", listenAddr, err)
	}
	return &Server{listener: listener}, nil
}

// Register builds the underlying [grpc.Server] with opts and registers the
// fibre service. It must be called exactly once before [Server.Serve].
func (s *Server) Register(service types.FibreServer, opts ...grpc.ServerOption) {
	s.server = grpc.NewServer(opts...)
	types.RegisterFibreServer(s.server, service)
}

// ListenAddress returns the actual address the server is listening on.
func (s *Server) ListenAddress() string {
	return s.listener.Addr().String()
}

// Serve starts serving gRPC requests in a background goroutine.
// [Server.Register] must have been called first.
func (s *Server) Serve() {
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		_ = s.server.Serve(s.listener)
	}()
}

// Stop gracefully stops the gRPC server.
// If the context is cancelled before draining completes, it forces an immediate stop.
// If [Server.Register] was never called, Stop closes the listener and returns.
func (s *Server) Stop(ctx context.Context) {
	if s.server == nil {
		_ = s.listener.Close()
		return
	}

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

	if s.done != nil {
		<-s.done
	}
}
