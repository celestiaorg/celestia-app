package fibre

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestPackedMetricsCountHTTPRetries(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "<Error><Code>SlowDown</Code><Message>Please reduce your request rate</Message></Error>")
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	metrics, err := newServerMetrics(provider.Meter("packed-test"), newOccupancy(0))
	require.NoError(t, err)
	b, _ := newPackedTestBackend(t, 1)
	b.object.metrics = metrics
	b.object.client = s3.NewFromConfig(aws.Config{
		Region:      "eu-west-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
		Retryer: func() aws.Retryer {
			return retry.NewStandard(func(o *retry.StandardOptions) { o.MaxAttempts = 2; o.MaxBackoff = time.Millisecond })
		},
	}, func(o *s3.Options) { o.BaseEndpoint = aws.String(server.URL); o.UsePathStyle = true })
	require.NoError(t, b.Put(t.Context(), Commitment{}, []byte{7}, &types.BlobShard{Rlcs: []byte("data")}))
	require.Equal(t, int64(2), attempts.Load())
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	seen := 0
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "fibre.server.storage.packed.put.attempts":
				var total int64
				for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
					total += point.Value
				}
				require.Equal(t, int64(2), total)
				seen++
			case "fibre.server.storage.packed.shards":
				points := m.Data.(metricdata.Histogram[int64]).DataPoints
				require.Len(t, points, 1)
				require.Equal(t, uint64(1), points[0].Count)
				require.Equal(t, int64(1), points[0].Sum)
				seen++
			}
		}
	}
	require.Equal(t, 2, seen)
}
