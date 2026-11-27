package version

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/log"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	sdkversion "github.com/cosmos/cosmos-sdk/version"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

func activeStatus() *ctypes.ResultStatus {
	return &ctypes.ResultStatus{ValidatorInfo: ctypes.ValidatorInfo{Address: []byte{0xAB, 0xCD}, VotingPower: 10}}
}

func decodeReport(t *testing.T, r *http.Request) *collectormetrics.ExportMetricsServiceRequest {
	t.Helper()
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-protobuf" {
		t.Errorf("unexpected OTLP request: %s %s", r.Method, r.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
	}
	var report collectormetrics.ExportMetricsServiceRequest
	if err := proto.Unmarshal(body, &report); err != nil {
		t.Error(err)
	}
	return &report
}

func TestImmediateReport(t *testing.T) {
	for _, prefix := range []string{"", "/", "/otel", "/otel/"} {
		t.Run(prefix, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var report *collectormetrics.ExportMetricsServiceRequest
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				want := "/v1/metrics"
				if len(prefix) > 1 {
					want = "/otel/v1/metrics"
				}
				if r.URL.Path != want {
					t.Errorf("path = %s, want %s", r.URL.Path, want)
				}
				mu.Lock()
				report = decodeReport(t, r)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-protobuf")
				cancel()
			}))
			defer server.Close()
			globalProvider := otel.GetMeterProvider()
			start := time.Now().Unix()
			require.NoError(t, run(ctx, config{server.URL + prefix, time.Hour}, "test-chain", func() (*ctypes.ResultStatus, error) {
				return activeStatus(), nil
			}, log.NewNopLogger()))
			require.Same(t, globalProvider, otel.GetMeterProvider())
			mu.Lock()
			defer mu.Unlock()
			require.NotNil(t, report, "first export must not wait for the interval")
			require.Len(t, report.ResourceMetrics, 1)
			scopes := report.ResourceMetrics[0].ScopeMetrics
			require.Len(t, scopes, 1)
			require.Len(t, scopes[0].Metrics, 2, "only version metrics may be exported")
			values := map[string]int64{}
			for _, m := range scopes[0].Metrics {
				require.NotNil(t, m.GetGauge())
				require.Len(t, m.GetGauge().DataPoints, 1)
				point := m.GetGauge().DataPoints[0]
				require.Equal(t, map[string]string{
					"chain_id": "test-chain", "consensus_address": "ABCD",
					"version": sdkversion.Version, "commit": sdkversion.Commit,
				}, stringAttributes(point.Attributes))
				values[m.Name] = point.GetAsInt()
			}
			require.EqualValues(t, 1, values["celestia_validator_build_info"])
			require.GreaterOrEqual(t, values["celestia_validator_report_timestamp_seconds"], start)
		})
	}
}

func stringAttributes(attrs []*common.KeyValue) map[string]string {
	result := make(map[string]string, len(attrs))
	for _, a := range attrs {
		result[a.Key] = a.Value.GetStringValue()
	}
	return result
}

func TestMembershipTransitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	inactive := activeStatus()
	inactive.ValidatorInfo.VotingPower = 0
	syncing := activeStatus()
	syncing.SyncInfo.CatchingUp = true
	missingAddress := activeStatus()
	missingAddress.ValidatorInfo.Address = nil
	sequence := []struct {
		status *ctypes.ResultStatus
		err    error
	}{
		{inactive, nil},
		{syncing, nil},
		{nil, errors.New("state unavailable")},
		{nil, nil},
		{missingAddress, nil},
		{activeStatus(), nil},
		{inactive, nil},
		{activeStatus(), nil},
		{syncing, nil},
	}
	var mu sync.Mutex
	var reports []*collectormetrics.ExportMetricsServiceRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reports = append(reports, decodeReport(t, r))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer server.Close()
	i := 0
	status := func() (*ctypes.ResultStatus, error) {
		if i == len(sequence) {
			cancel()
			return nil, nil
		}
		s := sequence[i]
		i++
		return s.status, s.err
	}
	require.NoError(t, run(ctx, config{server.URL, time.Millisecond}, "test-chain", status, log.NewNopLogger()))
	require.Equal(t, len(sequence), i)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, reports, 2, "only the two active, caught-up checks should send requests")
	for _, report := range reports {
		require.Len(t, report.ResourceMetrics[0].ScopeMetrics[0].Metrics, 2)
	}
}

func TestCollectorFailureRetriesNextInterval(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeReport(t, r)
		mu.Lock()
		defer mu.Unlock()
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.Header().Set("Content-Type", "application/x-protobuf")
		}
	}))
	defer server.Close()
	checks := 0
	require.NoError(t, run(ctx, config{server.URL, time.Millisecond}, "test-chain", func() (*ctypes.ResultStatus, error) {
		checks++
		if checks == 3 {
			cancel()
			return nil, nil
		}
		return activeStatus(), nil
	}, log.NewNopLogger()))
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 2, requests)
}

func TestCancellationInterruptsExport(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		decodeReport(t, r)
		close(started)
		<-release
	}))
	defer server.Close()
	defer close(release)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, config{server.URL, time.Hour}, "test-chain", func() (*ctypes.ResultStatus, error) {
			return activeStatus(), nil
		}, log.NewNopLogger())
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("report did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt the export")
	}
}
