package fibre

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"testing/iotest"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestBackendGetMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	metrics, err := newServerMetrics(provider.Meter("test"), newOccupancy(0))
	require.NoError(t, err)
	done := metrics.observeBackendGet(t.Context(), storageBackendObject)
	source := io.MultiReader(bytes.NewBufferString("data"), iotest.ErrReader(context.DeadlineExceeded))
	data, err := io.ReadAll(metrics.backendReader(t.Context(), storageBackendObject, source))
	require.Equal(t, "data", string(data))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	done(err)

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	seen := 0
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "fibre.server.backend.get.duration":
				points := m.Data.(metricdata.Histogram[float64]).DataPoints
				require.Len(t, points, 1)
				require.Equal(t, uint64(1), points[0].Count)
				require.Positive(t, points[0].Sum)
				require.Equal(t, attribute.NewSet(attribute.String("backend", "object"), attribute.String("outcome", "timeout")), points[0].Attributes)
			case "fibre.server.backend.get.in_flight", "fibre.server.backend.get.bytes":
				points := m.Data.(metricdata.Sum[int64]).DataPoints
				require.Len(t, points, 1)
				want := int64(0)
				if m.Name == "fibre.server.backend.get.bytes" {
					want = 4
				}
				require.Equal(t, want, points[0].Value)
				require.Equal(t, attribute.NewSet(attribute.String("backend", "object")), points[0].Attributes)
			default:
				continue
			}
			seen++
		}
	}
	require.Equal(t, 3, seen)
}

func TestBackendGetOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		err           error
	}{
		{"success", "success", nil},
		{"missing", "not_found", ErrStoreNotFound},
		{"deadline", "timeout", context.DeadlineExceeded},
		{"network timeout", "timeout", &net.OpError{Err: os.ErrDeadlineExceeded}},
		{"canceled", "canceled", context.Canceled},
		{"slow down", "throttled", &smithy.GenericAPIError{Code: "SlowDown"}},
		{"429", "throttled", &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusTooManyRequests}},
			Err:      errors.New("too many requests"),
		}},
		{"other", "error", errors.New("arbitrary error text")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err
			if err != nil {
				err = fmt.Errorf("wrapped: %w", err)
			}
			require.Equal(t, tc.outcome, backendGetOutcome(err))
		})
	}
}

func TestBackendMetricsDisabled(t *testing.T) {
	disabled, err := newServerMetrics(noop.NewMeterProvider().Meter("test"), newOccupancy(0))
	require.NoError(t, err)
	for _, metrics := range []*serverMetrics{nil, disabled} {
		r := bytes.NewReader([]byte("data"))
		require.Same(t, r, metrics.backendReader(t.Context(), storageBackendLocal, r))
		metrics.observeBackendGet(t.Context(), storageBackendLocal)(nil)
	}
}
