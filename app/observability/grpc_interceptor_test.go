
package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func resetMetrics() {
	grpcRequestsTotal.Reset()
	grpcRequestDurationSeconds.Reset()
	grpcRequestsInFlight.Reset()
}

func TestUnaryInterceptor_RecordsOK(t *testing.T) {
	t.Cleanup(resetMetrics)

	interceptor := UnaryPrometheusInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}
	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }

	resp, err := interceptor(context.Background(), nil, info, handler)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)

	got := testutil.ToFloat64(grpcRequestsTotal.WithLabelValues(
		"cosmos.bank.v1beta1.Query", "Balance", "unary", "OK"))
	require.Equal(t, 1.0, got)

	require.Equal(t, 1, testutil.CollectAndCount(grpcRequestDurationSeconds))
}

func TestUnaryInterceptor_RecordsStatusCode(t *testing.T) {
	t.Cleanup(resetMetrics)

	interceptor := UnaryPrometheusInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(codes.NotFound, "missing")
	}

	_, err := interceptor(context.Background(), nil, info, handler)
	require.Error(t, err)

	got := testutil.ToFloat64(grpcRequestsTotal.WithLabelValues(
		"cosmos.bank.v1beta1.Query", "Balance", "unary", "NotFound"))
	require.Equal(t, 1.0, got)
}

func TestUnaryInterceptor_NonStatusErrorIsUnknown(t *testing.T) {
	t.Cleanup(resetMetrics)

	interceptor := UnaryPrometheusInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, errors.New("weird err")
	}

	_, err := interceptor(context.Background(), nil, info, handler)
	require.Error(t, err)

	got := testutil.ToFloat64(grpcRequestsTotal.WithLabelValues(
		"cosmos.bank.v1beta1.Query", "Balance", "unary", "Unknown"))
	require.Equal(t, 1.0, got)
}

func TestUnaryInterceptor_TracksInFlight(t *testing.T) {
	t.Cleanup(resetMetrics)

	interceptor := UnaryPrometheusInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}

	var duringHandler float64
	handler := func(ctx context.Context, req any) (any, error) {
		duringHandler = testutil.ToFloat64(grpcRequestsInFlight.WithLabelValues(
			"cosmos.bank.v1beta1.Query", "Balance", "unary"))
		return nil, nil
	}

	_, err := interceptor(context.Background(), nil, info, handler)
	require.NoError(t, err)

	require.Equal(t, 1.0, duringHandler)

	after := testutil.ToFloat64(grpcRequestsInFlight.WithLabelValues(
		"cosmos.bank.v1beta1.Query", "Balance", "unary"))
	require.Equal(t, 0.0, after)
}

func TestSplitFullMethod(t *testing.T) {
	tests := []struct {
		name           string
		fullMethod     string
		wantService    string
		wantMethod     string
	}{
		{
			name:        "standard",
			fullMethod:  "/cosmos.bank.v1beta1.Query/Balance",
			wantService: "cosmos.bank.v1beta1.Query",
			wantMethod:  "Balance",
		},
		{
			name:        "missing slash",
			fullMethod:  "Balance",
			wantService: "unknown",
			wantMethod:  "Balance",
		},
		{
			name:        "empty",
			fullMethod:  "",
			wantService: "unknown",
			wantMethod:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, method := splitFullMethod(tt.fullMethod)
			require.Equal(t, tt.wantService, service)
			require.Equal(t, tt.wantMethod, method)
		})
	}
}
