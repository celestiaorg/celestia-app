package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
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

func TestOTelSignalPaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		base   string
		prefix string
	}{
		{name: "root"},
		{name: "trailing slash", base: "/"},
		{name: "full metrics path", base: "/v1/metrics"},
		{name: "full trace path", base: "/v1/traces"},
		{name: "proxy prefix", base: "/otel", prefix: "/otel"},
		{name: "proxy prefix with trailing slash", base: "/otel/", prefix: "/otel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/x-protobuf", r.Header.Get("Content-Type"))
				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			tracerProvider := otel.GetTracerProvider()
			meterProvider := otel.GetMeterProvider()
			propagator := otel.GetTextMapPropagator()
			t.Cleanup(func() {
				otel.SetTracerProvider(tracerProvider)
				otel.SetMeterProvider(meterProvider)
				otel.SetTextMapPropagator(propagator)
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			defer func() {
				mu.Lock()
				defer mu.Unlock()
				assert.ElementsMatch(t, []string{tc.prefix + "/v1/traces", tc.prefix + "/v1/metrics"}, paths)
			}()
			shutdownTracing, err := setupOTelTracing(ctx, server.URL+tc.base)
			require.NoError(t, err)
			defer shutdownTracing(ctx)
			shutdownMetrics, err := setupOTelMetrics(ctx, server.URL+tc.base)
			require.NoError(t, err)
			defer shutdownMetrics(ctx)

			// A sampled parent makes export independent of root span sampling.
			parent := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    trace.TraceID{1},
				SpanID:     trace.SpanID{1},
				TraceFlags: trace.FlagsSampled,
				Remote:     true,
			})
			_, span := otel.Tracer("test").Start(trace.ContextWithRemoteSpanContext(ctx, parent), "test")
			span.End()
			counter, err := otel.Meter("test").Int64Counter("test_counter")
			require.NoError(t, err)
			counter.Add(ctx, 1)
		})
	}
}
