package fibre_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestServerUploadShardLastSuccess checks that the last-success timestamp is
// absent before the first successful upload, is not set by a failed upload,
// and is set by a successful one.
func TestServerUploadShardLastSuccess(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	server, valSet, serverValidator := makeTestServerWithConfig(t, func(cfg *fibre.ServerConfig) {
		cfg.Meter = sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	})

	_, ok := lastUploadSuccess(t, reader)
	require.False(t, ok, "must not be reported before the first upload")

	bad := makeTestRequest(t, valSet, serverValidator, nil)
	bad.Promise = nil
	_, err := server.UploadShard(t.Context(), bad)
	require.Error(t, err)
	_, ok = lastUploadSuccess(t, reader)
	require.False(t, ok, "must not be reported after a failed upload")

	before := time.Now().Unix()
	_, err = server.UploadShard(t.Context(), makeTestRequest(t, valSet, serverValidator, nil))
	require.NoError(t, err)
	ts, ok := lastUploadSuccess(t, reader)
	require.True(t, ok, "must be reported after a successful upload")
	require.GreaterOrEqual(t, ts, before)
	require.LessOrEqual(t, ts, time.Now().Unix())
}

// lastUploadSuccess returns the upload_shard.last_success_timestamp gauge and
// whether it was reported.
func lastUploadSuccess(t *testing.T, reader sdkmetric.Reader) (int64, bool) {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "fibre.server.upload_shard.last_success_timestamp" {
				continue
			}
			points := m.Data.(metricdata.Gauge[int64]).DataPoints
			if len(points) == 0 {
				return 0, false
			}
			require.Len(t, points, 1)
			return points[0].Value, true
		}
	}
	return 0, false
}
