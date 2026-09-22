package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestConfirmationAccounting(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer provider.Shutdown(context.Background())
	st := &stats{}
	m, err := newTxsimMetrics(provider.Meter("test"), st)
	require.NoError(t, err)
	st.metrics = m
	defer m.registration.Unregister()
	ch := make(chan confirmRequest, 1)
	req := confirmRequest{rawBytes: 100, paddedBytes: 256}
	enqueueConfirmation(context.Background(), ch, req, st)
	enqueueConfirmation(context.Background(), ch, req, st)
	enqueueConfirmation(context.Background(), nil, req, st)
	require.EqualValues(t, 1, st.pending.Load())
	require.EqualValues(t, 2, st.untracked.Load())
	queued := <-ch
	require.EqualValues(t, 100, queued.rawBytes)
	require.EqualValues(t, 256, queued.paddedBytes)
	m.record(context.Background(), "upload", "success", 100, 256)
	m.record(context.Background(), "broadcast", "success", 100, 256)
	m.record(context.Background(), "confirmation", "success", queued.rawBytes, queued.paddedBytes)
	st.pending.Add(-1)
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	want := map[string]int64{"fibre.txsim.blobs": 1, "fibre.txsim.raw_bytes": 100, "fibre.txsim.padded_bytes": 256}
	found := 0
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			expected, ok := want[m.Name]
			if !ok {
				continue
			}
			found++
			sum := m.Data.(metricdata.Sum[int64])
			require.Len(t, sum.DataPoints, 4)
			for _, point := range sum.DataPoints {
				value := expected
				outcome, _ := point.Attributes.Value("outcome")
				if outcome.AsString() == "untracked" {
					value *= 2
				}
				require.Equal(t, value, point.Value)
			}
		}
	}
	require.Equal(t, 3, found)
}

func TestConfirmationOutcome(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "success"}, {fmt.Errorf("wrap: %w", errTxRejected), "failed"},
		{context.DeadlineExceeded, "timeout"}, {errTxEvicted, "unknown"},
		{context.Canceled, "unknown"},
	} {
		require.Equal(t, tc.want, confirmationOutcome(tc.err))
	}
}

func TestParseRSS(t *testing.T) {
	n, err := parseRSS("200 100 50", 4096)
	require.NoError(t, err)
	require.EqualValues(t, 409600, n)
	for _, s := range []string{"", "100", "100 nope", "100 -1", "100 9223372036854775807"} {
		_, err := parseRSS(s, 4096)
		require.Error(t, err)
	}
}
