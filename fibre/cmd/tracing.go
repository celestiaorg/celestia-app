package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	otelpyroscope "github.com/grafana/otel-profiling-go"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	flagOTelEndpoint    = "otel-endpoint"
	envOTelEndpoint     = "FIBRE_OTEL_ENDPOINT"
	defaultOTelEndpoint = ""
)

// registerTracingFlags adds the otel-endpoint persistent flag to cmd and
// applies the corresponding environment variable override if set.
func registerTracingFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(flagOTelEndpoint, defaultOTelEndpoint, fmt.Sprintf("OpenTelemetry OTLP HTTP endpoint for tracing, e.g. http://localhost:4318 (or set %s)", envOTelEndpoint))
	setPersistentFlagFromEnv(cmd, flagOTelEndpoint, envOTelEndpoint)
}

// setupTracing reads the otel-endpoint flag from cmd and initializes the global
// OpenTelemetry tracer provider with an OTLP HTTP exporter. If the endpoint is
// empty, no SDK is started and a no-op shutdown is returned. When tracing is
// active, the provider is automatically wrapped with otelpyroscope so that pprof
// goroutine labels carry span IDs — enabling trace-profile correlation in Grafana
// whenever Pyroscope profiling is also enabled. The shutdown function must be
// called on exit to flush buffered spans.
func setupTracing(ctx context.Context, cmd *cobra.Command) (func(context.Context), error) {
	endpoint, err := cmd.Flags().GetString(flagOTelEndpoint)
	if err != nil {
		return nil, fmt.Errorf("get %q flag: %w", flagOTelEndpoint, err)
	}
	if endpoint == "" {
		return func(context.Context) {}, nil
	}

	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	hostname, _ := os.Hostname()
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("fibre"),
		semconv.ServiceVersion(version),
		semconv.ServiceInstanceID(hostname),
	))
	if err != nil {
		return nil, fmt.Errorf("creating OTel resource: %w", err)
	}

	// ParentBased respects sampling decisions from upstream services.
	// TraceIDRatioBased(0.1) samples 10% of root spans — a standard production default.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))),
	)
	// Wrap with otelpyroscope so pprof samples are automatically annotated with
	// span IDs whenever the Pyroscope profiler is running alongside.
	otel.SetTracerProvider(otelpyroscope.NewTracerProvider(tp))
	// W3C TraceContext propagates trace/span IDs across HTTP and gRPC boundaries;
	// Baggage carries key-value pairs through the trace.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	slog.Info("tracing enabled", "endpoint", endpoint)

	return func(ctx context.Context) {
		if err := tp.Shutdown(ctx); err != nil {
			slog.Error("shutting down tracer provider", "error", err)
		}
	}, nil
}

// setPersistentFlagFromEnv sets a persistent flag value from an environment
// variable, exiting if the value is invalid.
func setPersistentFlagFromEnv(cmd *cobra.Command, flagName, envVar string) {
	if val, ok := os.LookupEnv(envVar); ok && val != "" {
		if err := cmd.PersistentFlags().Lookup(flagName).Value.Set(val); err != nil {
			fmt.Printf("Error setting %s from %s: %v\n", flagName, envVar, err)
			os.Exit(1)
		}
	}
}
