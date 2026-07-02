package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
//
// A panic-recovery interceptor is always installed as defense in depth: a
// panic in any handler (e.g. a malformed request that slips past validation)
// is converted into an Internal gRPC error instead of crashing the process.
func (s *Server) Register(service types.FibreServer, opts ...grpc.ServerOption) {
	opts = append(opts, grpc.ChainUnaryInterceptor(recoverUnaryInterceptor))
	s.server = grpc.NewServer(opts...)
	types.RegisterFibreServer(s.server, service)
}

// recoverUnaryInterceptor recovers from panics in unary handlers and returns an
// Internal error so a single malformed request cannot crash the server process.
func recoverUnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("recovered from panic in gRPC handler",
				"method", info.FullMethod,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			err = status.Errorf(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
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

	// Registered but never served: grpc-go only takes ownership of the listener
	// in Serve, so GracefulStop/Stop would not close it. Close it ourselves to
	// avoid leaking the file descriptor and port on a failed startup.
	if s.done == nil {
		s.server.Stop()
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

	<-s.done
}
