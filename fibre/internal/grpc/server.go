package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"golang.org/x/net/netutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
	"google.golang.org/protobuf/proto"
)

// Connection and stream caps bound how many requests run at once; receive
// memory is bounded by request admission (see admission.go and ADR-031).
//
// NewServerCodec separately limits rows and proofs before decoding allocates
// memory for them, and rejects oversized DownloadShard requests before copying
// them.
const (
	// DefaultMaxConnections is the default total connection cap.
	DefaultMaxConnections = 16
	// DefaultMaxConcurrentStreams is the default per-connection stream cap.
	DefaultMaxConcurrentStreams = 13

	// Fixed receive windows (>=65535) disable BDP tuning; the stream window only
	// bounds a paused stream, since RecvMsg grants a whole-message window.
	initialWindowSize     = 64 << 10
	initialConnWindowSize = 16 << 20

	// connectionTimeout bounds TCP+TLS+HTTP/2 setup so a peer cannot pin a
	// LimitListener slot with a stalled handshake for the 120s gRPC default.
	connectionTimeout = 15 * time.Second

	// KeepAlive drops idle or abusive connections.
	keepAliveMinTime     = 10 * time.Second // reject clients that ping more often
	keepAliveMaxConnIdle = 30 * time.Second // close idle connections
	keepAlivePingTime    = 2 * time.Minute  // ping interval to detect dead peers
	keepAlivePingTimeout = 20 * time.Second // ping ack deadline before drop
)

// Connection age caps how long a peer can hold a slot with streams that never
// finish, which MaxConnectionIdle does not cover. The grace lets slow in-flight
// uploads complete. Vars so tests can shorten them.
var (
	keepAliveMaxConnAge      = 5 * time.Minute
	keepAliveMaxConnAgeGrace = 2 * time.Minute
)

// Server wraps a [grpc.Server] with TCP listener and lifecycle management.
type Server struct {
	server               *grpc.Server
	listener             net.Listener
	adm                  *admission
	done                 chan struct{} // closed when Serve returns
	err                  error         // why Serve returned; valid once done is closed
	maxConcurrentStreams uint32
}

// Listen creates a [Server] bound to listenAddr that admits requests per admCfg.
// The underlying [grpc.Server] is created lazily by [Server.Register] so callers
// can defer building credentials until after the listener address is known
// (e.g., for TLS certs that depend on a chain ID resolved at startup).
func Listen(listenAddr string, maxConnections, maxConcurrentStreams int, admCfg AdmissionConfig) (*Server, error) {
	adm, err := newAdmission(admCfg)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", listenAddr, err)
	}
	// Cap total connections so a peer cannot dodge the per-connection stream cap
	// by opening many connections.
	listener = netutil.LimitListener(listener, maxConnections)
	return &Server{listener: listener, adm: adm, maxConcurrentStreams: uint32(maxConcurrentStreams)}, nil
}

// Register builds the underlying [grpc.Server] with opts and registers the
// fibre service. It must be called exactly once before [Server.Serve].
func (s *Server) Register(service types.FibreServer, opts ...grpc.ServerOption) {
	opts = append(opts,
		grpc.MaxConcurrentStreams(s.maxConcurrentStreams),
		grpc.InitialWindowSize(initialWindowSize),
		grpc.InitialConnWindowSize(initialConnWindowSize),
		grpc.ConnectionTimeout(connectionTimeout),
		grpc.InTapHandle(deadlineTap),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime: keepAliveMinTime,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     keepAliveMaxConnIdle,
			MaxConnectionAge:      keepAliveMaxConnAge,
			MaxConnectionAgeGrace: keepAliveMaxConnAgeGrace,
			Time:                  keepAlivePingTime,
			Timeout:               keepAlivePingTimeout,
		}),
	)
	s.server = grpc.NewServer(opts...)
	s.RegisterService(&types.Fibre_serviceDesc, service)
}

// RegisterService implements [grpc.ServiceRegistrar] for extra services such as
// gRPC health, admitting their requests too. Use it between Register and Serve.
func (s *Server) RegisterService(sd *grpc.ServiceDesc, impl any) {
	s.server.RegisterService(admitServiceDesc(sd, s.adm), impl)
}

// deadlineTap bounds server-side handling of one request. grpc uses the returned
// context as the stream context, so a stalled RecvMsg returns when it expires.
func deadlineTap(ctx context.Context, _ *tap.Info) (context.Context, error) {
	ctx, cancel := context.WithTimeout(ctx, requestDeadline)
	context.AfterFunc(ctx, cancel)
	return ctx, nil
}

// admitServiceDesc serves every unary method of sd through admitHandler. Streaming
// methods are left unregistered (Fibre has none; health Watch is unsupported), so
// grpc rejects them as Unimplemented before reading a body.
func admitServiceDesc(sd *grpc.ServiceDesc, adm *admission) *grpc.ServiceDesc {
	out := *sd
	out.Methods, out.Streams = nil, nil
	for _, m := range sd.Methods {
		out.Streams = append(out.Streams, grpc.StreamDesc{StreamName: m.MethodName, Handler: admitHandler(m, adm)})
	}
	return &out
}

// admitHandler serves a unary method as a stream handler so the request is
// admitted before it is read; the generated handler's dec reads it. Unary
// interceptors do not run here, so panics are recovered inline.
func admitHandler(m grpc.MethodDesc, adm *admission) grpc.StreamHandler {
	return func(srv any, ss grpc.ServerStream) (err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("recovered from panic in gRPC handler", "method", m.MethodName, "panic", r, "stack", string(debug.Stack()))
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()

		ctx := ss.Context()
		res, err := adm.admit(ctx, m.MethodName, peerKey(ctx))
		if err != nil {
			return err
		}
		defer res.release()

		dec := func(v any) error {
			if err := ss.RecvMsg(v); err != nil {
				return err
			}
			res.received(msgSize(v))
			return nil
		}
		resp, err := m.Handler(srv, ctx, dec, nil)
		if err != nil {
			return err
		}
		return ss.SendMsg(resp)
	}
}

// peerKey buckets the request's source IP (IPv6 by /64) for the per-IP cap.
func peerKey(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok {
		return connKey(p.Addr)
	}
	return ""
}

// msgSize is the decoded wire size of a request message.
func msgSize(v any) int64 {
	if s, ok := v.(interface{ Size() int }); ok { // gogoproto
		return int64(s.Size())
	}
	if m, ok := v.(proto.Message); ok { // protobuf v2 (e.g. health)
		return int64(proto.Size(m))
	}
	return 0
}

// Done is closed once Serve returned. Err then says why; nil after Stop.
func (s *Server) Done() <-chan struct{} { return s.done }

// Err returns the error Serve returned with. Only valid once Done is closed.
func (s *Server) Err() error { return s.err }

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
		s.err = s.server.Serve(s.listener) // nil after Stop or GracefulStop
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
