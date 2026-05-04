package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/server"
	sdkversion "github.com/cosmos/cosmos-sdk/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	otelprom "go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	flagOTelEndpoint = "otel-endpoint"
	envOTelEndpoint  = "CELESTIA_APP_OTEL_ENDPOINT"

	// Without a positive Prometheus retention the Cosmos SDK telemetry sink is
	// never registered on prometheus.DefaultRegisterer, so the bridge would have
	// no application-level metrics to scrape.
	otelDefaultPrometheusRetention = 60

	otelShutdownTimeout = 5 * time.Second
)

var otelEndpointFlagDescription = fmt.Sprintf(
	"OTLP HTTP endpoint of an OpenTelemetry collector (e.g. http://localhost:4318). "+
		"When set, the node redirects all Prometheus-collected metrics — Cosmos SDK "+
		"telemetry, CometBFT instrumentation, and Go runtime — to this collector via "+
		"push instead of relying on Prometheus pull. Auto-enables [telemetry] and "+
		"CometBFT [instrumentation].prometheus so the registry is populated. Existing "+
		"pull endpoints keep working and may be disabled separately. Env: %s",
	envOTelEndpoint,
)

// otelMeterProvider holds the active provider between the start command's
// PreRunE (where it is constructed) and PostRunE (where it is flushed and
// shut down). Process-global because the start command runs once per process.
var otelMeterProvider *sdkmetric.MeterProvider

func addOTelMetricsFlag(startCmd *cobra.Command) {
	startCmd.Flags().String(flagOTelEndpoint, "", otelEndpointFlagDescription)
}

func resolveOTelEndpoint(cmd *cobra.Command) (string, error) {
	endpoint, err := cmd.Flags().GetString(flagOTelEndpoint)
	if err != nil {
		return "", fmt.Errorf("get %q flag: %w", flagOTelEndpoint, err)
	}
	if endpoint == "" {
		endpoint = os.Getenv(envOTelEndpoint)
	}
	return endpoint, nil
}

func setupOTelMetrics(cmd *cobra.Command, logger log.Logger) error {
	endpoint, err := resolveOTelEndpoint(cmd)
	if err != nil {
		return err
	}
	if endpoint == "" {
		return nil
	}

	sctx := server.GetServerContextFromCmd(cmd)
	sctx.Viper.Set("telemetry.enabled", true)
	if sctx.Viper.GetInt64("telemetry.prometheus-retention-time") <= 0 {
		sctx.Viper.Set("telemetry.prometheus-retention-time", otelDefaultPrometheusRetention)
	}
	if sctx.Config != nil && sctx.Config.Instrumentation != nil {
		sctx.Config.Instrumentation.Prometheus = true
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint))
	if err != nil {
		return fmt.Errorf("create OTLP metric exporter: %w", err)
	}

	hostname, _ := os.Hostname()
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("celestia-appd"),
		semconv.ServiceVersion(sdkversion.Version),
		semconv.ServiceInstanceID(hostname),
	))
	if err != nil {
		return fmt.Errorf("create OTel resource: %w", err)
	}

	// Cosmos SDK telemetry's PrometheusSink and CometBFT's instrumentation both
	// register on prometheus.DefaultRegisterer, so a single bridge producer
	// captures both sources.
	promProducer := otelprom.NewMetricProducer(otelprom.WithGatherer(prometheus.DefaultGatherer))

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithProducer(promProducer))),
	)
	otel.SetMeterProvider(mp)

	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		_ = mp.Shutdown(ctx)
		return fmt.Errorf("start runtime metrics: %w", err)
	}

	otelMeterProvider = mp
	logger.Info("OTel metrics push enabled", "endpoint", endpoint)
	return nil
}

// shutdownOTelMetrics flushes and shuts down the meter provider on graceful
// start-command exit, after the SDK's own signal-driven shutdown has returned
// from RunE. It is wired as a PostRunE in addStartFlags.
func shutdownOTelMetrics(cmd *cobra.Command) {
	mp := otelMeterProvider
	if mp == nil {
		return
	}
	otelMeterProvider = nil

	ctx, cancel := context.WithTimeout(context.Background(), otelShutdownTimeout)
	defer cancel()
	if err := mp.Shutdown(ctx); err != nil {
		server.GetServerContextFromCmd(cmd).Logger.Error("shutting down OTel meter provider", "err", err)
	}
}
