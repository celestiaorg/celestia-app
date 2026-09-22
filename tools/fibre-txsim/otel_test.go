package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

func TestOTelExportPaths(t *testing.T) {
	for _, suffix := range []string{"", "/", "/collector/"} {
		t.Run(suffix, func(t *testing.T) {
			paths := make(chan string, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, err := io.Copy(io.Discard, r.Body)
				if err != nil {
					t.Errorf("read export body: %v", err)
				}
				paths <- r.URL.Path
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			previousMeter, previousTracer := otel.GetMeterProvider(), otel.GetTracerProvider()
			defer otel.SetMeterProvider(previousMeter)
			defer otel.SetTracerProvider(previousTracer)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stopMetrics, err := setupOTelMetrics(ctx, server.URL+suffix)
			require.NoError(t, err)
			stopTracing, err := setupOTelTracing(ctx, server.URL+suffix)
			require.NoError(t, err)
			counter, err := otel.Meter("test").Int64Counter("export_test")
			require.NoError(t, err)
			counter.Add(ctx, 1)
			_, span := otel.Tracer("test").Start(ctx, "export_test")
			span.End()
			stopMetrics(ctx)
			stopTracing(ctx)
			prefix := ""
			if suffix == "/collector/" {
				prefix = "/collector"
			}
			got := make([]string, 0, 2)
			for range 2 {
				select {
				case path := <-paths:
					got = append(got, path)
				case <-ctx.Done():
					t.Fatal("timed out waiting for OTLP exports")
				}
			}
			require.ElementsMatch(t, []string{prefix + "/v1/metrics", prefix + "/v1/traces"}, got)
		})
	}
}
