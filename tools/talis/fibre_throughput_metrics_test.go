package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestFibreThroughputMetrics(t *testing.T) {
	requests := make(chan *collectormetrics.ExportMetricsServiceRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/metrics" {
			t.Errorf("unexpected export path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request collectormetrics.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- &request
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(server.Close)

	counter, shutdown, err := setupFibreThroughputMetrics(t.Context(), server.URL)
	require.NoError(t, err)
	counter.Add(t.Context(), 100)
	counter.Add(t.Context(), 400)
	require.NoError(t, shutdown(t.Context()))

	select {
	case request := <-requests:
		require.Len(t, request.ResourceMetrics, 1)
		require.Len(t, request.ResourceMetrics[0].ScopeMetrics, 1)
		exported := request.ResourceMetrics[0].ScopeMetrics[0].Metrics
		require.Len(t, exported, 1)
		require.Equal(t, "fibre.pff.included.bytes", exported[0].Name)
		require.Equal(t, "By", exported[0].Unit)
		sum := exported[0].GetSum()
		require.NotNil(t, sum)
		require.True(t, sum.IsMonotonic)
		require.Equal(t, metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE, sum.AggregationTemporality)
		require.Len(t, sum.DataPoints, 1)
		require.EqualValues(t, 500, sum.DataPoints[0].GetAsInt())
	default:
		t.Fatal("no throughput metrics exported")
	}
}
