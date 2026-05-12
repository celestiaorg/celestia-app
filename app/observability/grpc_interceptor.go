package observability

import (
	"context"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	grpcRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_requests_total",
			Help: "Total number of gRPC server requests.",
		},
		[]string{"service", "method", "type", "code"},
	)

	grpcRequestDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_server_request_duration_seconds",
			Help:    "Duration of gRPC server requests in seconds.",
			Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		},
		[]string{"service", "method", "type", "code"},
	)

	grpcRequestsInFlight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grpc_server_requests_in_flight",
			Help: "Current number of in-flight gRPC server requests.",
		},
		[]string{"service", "method", "type"},
	)
)

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

func splitFullMethod(fullMethod string) (service, method string) {
	fullMethod = strings.TrimPrefix(fullMethod, "/")
	parts := strings.Split(fullMethod, "/")
	if len(parts) != 2 {
		return "unknown", fullMethod
	}
	return parts[0], parts[1]
}
