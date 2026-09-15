package main

import (
	"context"
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

func TestOTelSignalPaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		base   string
		prefix string
	}{
		{name: "root"},
		{name: "trailing slash", base: "/"},
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
			cmd := newRootCmd()
			require.NoError(t, cmd.ParseFlags([]string{"--" + flagOTelEndpoint, server.URL + tc.base}))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			defer func() {
				mu.Lock()
				defer mu.Unlock()
				assert.ElementsMatch(t, []string{tc.prefix + "/v1/traces", tc.prefix + "/v1/metrics"}, paths)
			}()
			shutdownTracing, err := setupTracing(ctx, cmd)
			require.NoError(t, err)
			defer shutdownTracing(ctx)
			shutdownMetrics, err := setupMetrics(ctx, cmd)
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
