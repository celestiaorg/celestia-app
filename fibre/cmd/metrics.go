package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// setupMetrics initializes the global OTel MeterProvider with an OTLP HTTP
// exporter pointing at the same endpoint used for tracing. If the endpoint is
// empty, no SDK is started and a no-op shutdown is returned. The shutdown
// function must be called on exit to flush buffered metrics.
func setupMetrics(ctx context.Context, cmd *cobra.Command) (func(context.Context), error) {
	endpoint, err := cmd.Flags().GetString(flagOTelEndpoint)
	if err != nil {
		return nil, fmt.Errorf("get %q flag: %w", flagOTelEndpoint, err)
	}
	if endpoint == "" {
		return func(context.Context) {}, nil
	}

	exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("creating OTLP metric exporter: %w", err)
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

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		return nil, fmt.Errorf("starting runtime metrics: %w", err)
	}

	slog.Info("metrics enabled", "endpoint", endpoint)

	return func(ctx context.Context) {
		if err := mp.Shutdown(ctx); err != nil {
			slog.Error("shutting down meter provider", "error", err)
		}
	}, nil
}
