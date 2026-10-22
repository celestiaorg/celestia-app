package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
	"google.golang.org/protobuf/proto"
)

// Connection caps are for safety and fairness only; request receive memory is
// bounded by the admission byte budget. See ADR-031.
const (
	// DefaultMaxConnections is the default value of the deprecated max_connections
	// knob (ignored; the global cap is fixed).
	DefaultMaxConnections = 0
	// DefaultMaxConnectionsPerIP caps connections per source IP (IPv6 by /64).
	DefaultMaxConnectionsPerIP = 16
	// DefaultMaxConcurrentStreams bounds per-connection stream concurrency only.
	DefaultMaxConcurrentStreams = 100
	// globalConnLimit is a fixed fd-safety cap, not a memory bound.
	globalConnLimit = 4096

	// Fixed receive windows (>=65535) disable BDP tuning; the stream window only
	// bounds a paused stream, since RecvMsg grants a whole-message window.
	uploadInitialWindowSize     = 64 << 10 // 64 KiB per stream
	uploadInitialConnWindowSize = 16 << 20 // 16 MiB per connection

	// connectionTimeout bounds the handshake so a stalled peer can't hold a slot
	// for the 120s gRPC default.
	connectionTimeout = 15 * time.Second

	keepAliveMinTime     = 10 * time.Second // reject clients that ping more often
	keepAliveMaxConnIdle = 30 * time.Second // close idle connections
	keepAlivePingTime    = 2 * time.Minute  // ping interval to detect dead peers
	keepAlivePingTimeout = 20 * time.Second // ping ack deadline before drop
)

// Cap how long a never-finishing stream holds a connection (MaxConnectionIdle
// misses that). Vars so tests can shorten them.
var (
	keepAliveMaxConnAge      = 5 * time.Minute
	keepAliveMaxConnAgeGrace = 2 * time.Minute
)

// Server wraps a [grpc.Server] with TCP listener and lifecycle management.
type Server struct {
	server               *grpc.Server
	listener             net.Listener
	connLimiter          *ConnLimiter
	adm                  *admission
	done                 chan struct{} // closed when Serve returns
	err                  error         // why Serve returned; valid once done is closed
	maxConcurrentStreams uint32
}

// Listen creates a [Server] bound to listenAddr. The underlying [grpc.Server]
// is created lazily by [Server.Register] so callers can defer building
// credentials until after the listener address is known (e.g., for TLS certs
// that depend on a chain ID resolved at startup). Connections are bounded by a
// fail-fast [ConnLimiter]: a fixed global fd-safety cap and a per-IP cap
// (maxConnectionsPerIP, 0 to disable).
func Listen(listenAddr string, maxConnectionsPerIP, maxConcurrentStreams int) (*Server, error) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", listenAddr, err)
	}
	limiter := newConnLimiter(globalConnLimit, maxConnectionsPerIP)
	return &Server{
		listener:             newLimitedListener(listener, limiter),
		connLimiter:          limiter,
		maxConcurrentStreams: uint32(maxConcurrentStreams),
	}, nil
}

// Register builds the [grpc.Server] and registers the fibre service, gating every
// method through memory admission (admCfg; a zero budget disables it). Call once
// before [Server.Serve].
func (s *Server) Register(service types.FibreServer, admCfg AdmissionConfig, opts ...grpc.ServerOption) {
	s.adm = newAdmission(admCfg)
	opts = append(opts,
		grpc.ChainUnaryInterceptor(recoverUnaryInterceptor),
		grpc.MaxConcurrentStreams(s.maxConcurrentStreams),
		grpc.InitialWindowSize(uploadInitialWindowSize),
		grpc.InitialConnWindowSize(uploadInitialConnWindowSize),
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
	s.server.RegisterService(admitServiceDesc(&types.Fibre_serviceDesc, s.adm), service)
}

// deadlineTap bounds server-side handling of one request just above the client's
// RPCTimeout. grpc uses the returned context as the stream context, so RecvMsg
// returns when it expires. AfterFunc releases the timer promptly once done.
func deadlineTap(ctx context.Context, _ *tap.Info) (context.Context, error) {
	ctx, cancel := context.WithTimeout(ctx, requestDeadline)
	context.AfterFunc(ctx, cancel)
	return ctx, nil
}

// Registrar exposes an admission-wrapping [grpc.ServiceRegistrar] for extra
// services such as gRPC health, so their requests are gated too.
func (s *Server) Registrar() grpc.ServiceRegistrar {
	return admittingRegistrar{inner: s.server, adm: s.adm}
}

type admittingRegistrar struct {
	inner grpc.ServiceRegistrar
	adm   *admission
}

func (r admittingRegistrar) RegisterService(sd *grpc.ServiceDesc, impl any) {
	r.inner.RegisterService(admitServiceDesc(sd, r.adm), impl)
}

// admitServiceDesc wraps every method of sd so admission reserves budget before
// the request is read. Unary methods become stream handlers (unary on the wire,
// so clients are unchanged); streaming methods gate each RecvMsg. A nil admission
// returns the descriptor unchanged.
func admitServiceDesc(sd *grpc.ServiceDesc, adm *admission) *grpc.ServiceDesc {
	if adm == nil {
		return sd
	}
	out := *sd
	out.Methods = nil
	out.Streams = nil
	for i := range sd.Methods {
		m := sd.Methods[i]
		h := unaryAdmitHandler(m, adm)
		if m.MethodName == downloadMethod {
			h = downloadAdmitHandler(m, adm) // also accounts the response
		}
		out.Streams = append(out.Streams, grpc.StreamDesc{StreamName: m.MethodName, Handler: h})
	}
	for i := range sd.Streams {
		d := sd.Streams[i]
		orig := d.Handler
		d.Handler = func(srv any, ss grpc.ServerStream) error {
			return orig(srv, &admittingStream{ServerStream: ss, adm: adm})
		}
		out.Streams = append(out.Streams, d)
	}
	return &out
}

// unaryAdmitHandler serves a unary method over a stream: admit, then run the
// generated handler whose dec reads the request. Panic recovery is inline because
// unary interceptors do not run on streams.
func unaryAdmitHandler(m grpc.MethodDesc, adm *admission) grpc.StreamHandler {
	return func(srv any, ss grpc.ServerStream) (err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("recovered from panic in gRPC handler", "method", m.MethodName, "panic", r, "stack", string(debug.Stack()))
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()

		ctx := ss.Context()
		res, err := adm.admit(ctx, peerKey(ctx))
		if err != nil {
			return err
		}
		defer res.release()

		dec := func(v any) error {
			if err := ss.RecvMsg(v); err != nil {
				return err
			}
			// Body received: shrink the max reservation to the decoded size and free
			// the per-IP receive slot.
			res.Shrink(msgSize(v))
			res.releaseRecv()
			return nil
		}
		resp, err := m.Handler(srv, ctx, dec, nil)
		if err != nil {
			return err
		}
		return ss.SendMsg(resp)
	}
}

const downloadMethod = "DownloadShard"

// downloadAdmitHandler is unaryAdmitHandler plus response accounting: it keeps the
// max reservation through the handler (which builds the shard), shrinks it to the
// response size, and releases it when the stream ends rather than at return, so a
// slow reader does not let the next download over-commit the budget.
func downloadAdmitHandler(m grpc.MethodDesc, adm *admission) grpc.StreamHandler {
	return func(srv any, ss grpc.ServerStream) (err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("recovered from panic in gRPC handler", "method", m.MethodName, "panic", r, "stack", string(debug.Stack()))
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()

		ctx := ss.Context()
		res, err := adm.admit(ctx, peerKey(ctx))
		if err != nil {
			return err
		}
		// Release when the stream ends (finishStream cancels ss.Context() after the
		// handler returns, http2_server.go). AfterFunc avoids a goroutine leak.
		context.AfterFunc(ctx, res.release)

		// Keep the max reservation through the handler so the response it builds is
		// counted; free only the per-IP receive slot after the request is read.
		dec := func(v any) error {
			if err := ss.RecvMsg(v); err != nil {
				return err
			}
			res.releaseRecv()
			return nil
		}
		resp, err := m.Handler(srv, ctx, dec, nil)
		if err != nil {
			return err
		}
		res.Shrink(msgSize(resp))
		return ss.SendMsg(resp)
	}
}

// admittingStream gates each RecvMsg on a streaming method so the request buffer
// is bounded without holding budget for the stream's lifetime.
type admittingStream struct {
	grpc.ServerStream
	adm *admission
}

func (s *admittingStream) RecvMsg(m any) error {
	res, err := s.adm.admit(s.Context(), peerKey(s.Context()))
	if err != nil {
		return err
	}
	defer res.release()
	return s.ServerStream.RecvMsg(m)
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
