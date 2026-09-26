package grpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"golang.org/x/net/netutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

// Connection and stream caps bound receive memory: gRPC buffers a full
// UploadShard message (~132 MiB) before the handler runs, so the worst case is
// maxConnections * maxConcurrentStreams * MaxRecvMsgSize (~27 GiB). The defaults
// are intentionally conservative for a 32 GiB-RAM validator; operators can
// override both caps via config to trade RAM for throughput.
//
// NewServerCodec separately limits rows and proofs before decoding allocates
// memory for them, and rejects oversized DownloadShard requests before copying
// them.
const (
	// DefaultMaxConnections is the default total connection cap.
	DefaultMaxConnections = 16
	// DefaultMaxConcurrentStreams is the default per-connection stream cap.
	DefaultMaxConcurrentStreams = 13

	// connectionTimeout bounds TCP+TLS+HTTP/2 setup so a peer cannot pin a
	// LimitListener slot with a stalled handshake for the 120s gRPC default.
	connectionTimeout = 15 * time.Second

	// KeepAlive drops idle or abusive connections.
	keepAliveMinTime     = 10 * time.Second // reject clients that ping more often
	keepAliveMaxConnIdle = 5 * time.Minute  // close idle connections
	keepAlivePingTime    = 2 * time.Minute  // ping interval to detect dead peers
	keepAlivePingTimeout = 20 * time.Second // ping ack deadline before drop
)

// Server wraps a [grpc.Server] with TCP listener and lifecycle management.
// Serve reports its exit through [Server.Done] and [Server.Err]; Stop is safe
// before Register, before Serve and when repeated.
type Server struct {
	server               *grpc.Server
	listener             net.Listener
	maxConcurrentStreams uint32

	mu       sync.Mutex
	served   bool
	stopped  bool
	stopOnce sync.Once
	done     chan struct{} // closed once the server can no longer serve
	err      error         // set before done is closed
}

// Listen creates a [Server] bound to listenAddr. The underlying [grpc.Server]
// is created lazily by [Server.Register] so callers can defer building
// credentials until after the listener address is known (e.g., for TLS certs
// that depend on a chain ID resolved at startup).
func Listen(listenAddr string, maxConnections, maxConcurrentStreams int) (*Server, error) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", listenAddr, err)
	}
	// Cap total connections so a peer cannot dodge the per-connection stream cap
	// by opening many connections.
	listener = netutil.LimitListener(listener, maxConnections)
	return &Server{listener: listener, maxConcurrentStreams: uint32(maxConcurrentStreams), done: make(chan struct{})}, nil
}

// Register builds the underlying [grpc.Server] with opts and registers the
// fibre service. It must be called exactly once before [Server.Serve].
//
// A panic-recovery interceptor is always installed as defense in depth: a
// panic in any handler (e.g. a malformed request that slips past validation)
// is converted into an Internal gRPC error instead of crashing the process.
func (s *Server) Register(service types.FibreServer, opts ...grpc.ServerOption) {
	opts = append(opts,
		grpc.ChainUnaryInterceptor(recoverUnaryInterceptor),
		grpc.MaxConcurrentStreams(s.maxConcurrentStreams),
		grpc.ConnectionTimeout(connectionTimeout),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime: keepAliveMinTime,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: keepAliveMaxConnIdle,
			Time:              keepAlivePingTime,
			Timeout:           keepAlivePingTimeout,
		}),
	)
	s.server = grpc.NewServer(opts...)
	types.RegisterFibreServer(s.server, service)
}

// Registrar exposes the [grpc.ServiceRegistrar] for extra services such as gRPC health; use it between Register and Serve.
func (s *Server) Registrar() grpc.ServiceRegistrar { return s.server }

// Done is closed once the server stopped serving; Err then reports why, nil for a clean stop.
func (s *Server) Done() <-chan struct{} { return s.done }

func (s *Server) Err() error {
	select {
	case <-s.done:
		return s.err
	default:
		return nil
	}
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
// [Server.Register] must have been called first. Repeated calls, or calls
// after Stop, do nothing.
func (s *Server) Serve() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.served || s.stopped || s.server == nil {
		return
	}
	s.served = true
	go func() {
		defer close(s.done)
		err := s.server.Serve(s.listener)
		s.mu.Lock()
		if s.stopped || errors.Is(err, grpc.ErrServerStopped) {
			err = nil // an exit caused by Stop is not a failure
		}
		s.mu.Unlock()
		s.err = err
	}()
}

// Stop gracefully stops the gRPC server and waits for it to exit. If the
// context ends before draining completes, it forces an immediate stop. Before
// Serve it only releases the listener, which grpc-go does not own yet.
func (s *Server) Stop(ctx context.Context) {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		served, server := s.served, s.server
		s.mu.Unlock()
		if !served {
			if server != nil {
				server.Stop()
			}
			_ = s.listener.Close()
			close(s.done)
			return
		}
		drained := make(chan struct{})
		go func() {
			server.GracefulStop()
			close(drained)
		}()
		select {
		case <-drained:
		case <-ctx.Done():
			server.Stop()
			<-drained
		}
	})
	<-s.done
}
