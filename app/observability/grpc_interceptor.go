package observability

import (
	"context"
	"strings"
	"sync"
	"time"

	servergrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	grpcRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_requests_total",
			Help: "Total number of gRPC server requests.",
		},
		[]string{"service", "method", "type", "code"},
	)

	grpcRequestDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_server_request_duration_seconds",
			Help:    "Duration of gRPC server requests in seconds.",
			Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
		},
		[]string{"service", "method", "type", "code"},
	)

	grpcRequestsInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grpc_server_requests_in_flight",
			Help: "Current number of in-flight gRPC server requests.",
		},
		[]string{"service", "method", "type"},
	)
)

// RegisterGRPCMetrics registers the gRPC server metrics on the supplied registerer.
// Pass prometheus.DefaultRegisterer to expose them on the standard /metrics endpoint.
func RegisterGRPCMetrics(reg prometheus.Registerer) {
	reg.MustRegister(
		grpcRequestsTotal,
		grpcRequestDurationSeconds,
		grpcRequestsInFlight,
	)
}

var setupOnce sync.Once

// Setup registers the gRPC server metrics on the default Prometheus registerer
// and installs the unary + stream Prometheus interceptors as extra options on
// the SDK gRPC server. Safe to call multiple times; runs only once per process.
func Setup() {
	setupOnce.Do(func() {
		RegisterGRPCMetrics(prometheus.DefaultRegisterer)
		servergrpc.ExtraServerOptions = append(
			servergrpc.ExtraServerOptions,
			grpc.ChainUnaryInterceptor(UnaryPrometheusInterceptor()),
			grpc.ChainStreamInterceptor(StreamPrometheusInterceptor()),
		)
	})
}

func UnaryPrometheusInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		service, method := splitFullMethod(info.FullMethod)
		rpcType := "unary"

		grpcRequestsInFlight.WithLabelValues(service, method, rpcType).Inc()
		defer grpcRequestsInFlight.WithLabelValues(service, method, rpcType).Dec()

		start := time.Now()
		resp, err := handler(ctx, req)

		code := status.Code(err).String()
		duration := time.Since(start).Seconds()

		grpcRequestsTotal.WithLabelValues(service, method, rpcType, code).Inc()
		grpcRequestDurationSeconds.WithLabelValues(service, method, rpcType, code).Observe(duration)

		return resp, err
	}
}

func StreamPrometheusInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		service, method := splitFullMethod(info.FullMethod)
		rpcType := streamType(info)

		grpcRequestsInFlight.WithLabelValues(service, method, rpcType).Inc()
		defer grpcRequestsInFlight.WithLabelValues(service, method, rpcType).Dec()

		start := time.Now()
		err := handler(srv, ss)

		code := status.Code(err).String()
		duration := time.Since(start).Seconds()

		grpcRequestsTotal.WithLabelValues(service, method, rpcType, code).Inc()
		grpcRequestDurationSeconds.WithLabelValues(service, method, rpcType, code).Observe(duration)

		return err
	}
}

func splitFullMethod(fullMethod string) (service, method string) {
	fullMethod = strings.TrimPrefix(fullMethod, "/")
	parts := strings.Split(fullMethod, "/")
	if len(parts) != 2 {
		return "unknown", fullMethod
	}
	return parts[0], parts[1]
}

func streamType(info *grpc.StreamServerInfo) string {
	switch {
	case info.IsClientStream && info.IsServerStream:
		return "bidi_stream"
	case info.IsClientStream:
		return "client_stream"
	case info.IsServerStream:
		return "server_stream"
	default:
		return "stream"
	}
}
