package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func setupFibreThroughputMetrics(ctx context.Context, endpoint string) (metric.Int64Counter, func(context.Context) error, error) {
	exporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint), otlpmetrichttp.WithURLPath("/v1/metrics"))
	if err != nil {
		return nil, nil, fmt.Errorf("creating throughput metric exporter: %w", err)
	}
	hostname, _ := os.Hostname()
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(10*time.Second))),
		sdkmetric.WithResource(resource.NewSchemaless(
			semconv.ServiceName("fibre-throughput"),
			semconv.ServiceInstanceID(hostname),
		)),
	)
	counter, err := provider.Meter("fibre-throughput").Int64Counter("fibre.pff.included.bytes",
		metric.WithDescription("Blob bytes in successfully executed PayForFibre transactions"),
		metric.WithUnit("By"),
	)
	if err != nil {
		_ = provider.Shutdown(ctx)
		return nil, nil, fmt.Errorf("creating PFF inclusion counter: %w", err)
	}
	counter.Add(ctx, 0)
	return counter, provider.Shutdown, nil
}
