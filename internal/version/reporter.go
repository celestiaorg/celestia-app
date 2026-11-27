package version

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"cosmossdk.io/log"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	sdkversion "github.com/cosmos/cosmos-sdk/version"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

const (
	defaultInterval = time.Minute
	exportTimeout   = 5 * time.Second
)

type config struct {
	endpoint string
	interval time.Duration
}

func (c config) validate() error {
	u, err := url.Parse(c.endpoint)
	if err != nil {
		return fmt.Errorf("invalid version-reporting endpoint: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("version-reporting endpoint must be an HTTP(S) base URL without credentials, query, or fragment")
	}
	if c.interval <= 0 {
		return fmt.Errorf("version-reporting interval must be positive")
	}
	return nil
}

// run checks eligibility before collecting or exporting anything. A manual reader
// avoids sending even empty OTLP requests for full nodes and inactive validators.
func run(ctx context.Context, cfg config, chainID string, status func() (*ctypes.ResultStatus, error), logger log.Logger) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	endpoint, err := url.JoinPath(cfg.endpoint, "v1/metrics")
	if err != nil {
		return err
	}
	exporter, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(endpoint),
		otlpmetrichttp.WithTimeout(exportTimeout),
		// The next periodic report is the retry; do not queue stale reports.
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}),
	)
	if err != nil {
		return err
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(resource.NewSchemaless(
			attribute.String("service.name", "celestia-app-version"),
		)),
	)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), exportTimeout)
		defer cancel()
		if err := provider.Shutdown(shutdownCtx); err != nil {
			logger.Warn("version metrics shutdown failed", "err", err)
		}
		if err := exporter.Shutdown(shutdownCtx); err != nil {
			logger.Warn("version exporter shutdown failed", "err", err)
		}
	}()
	meter := provider.Meter("celestia-app/version")
	buildInfo, err := meter.Int64Gauge("celestia_validator_build_info")
	if err != nil {
		return err
	}
	reportTime, err := meter.Int64Gauge("celestia_validator_report_timestamp_seconds")
	if err != nil {
		return err
	}

	report := func() error {
		s, err := status()
		if err != nil {
			return err
		}
		if s == nil || s.SyncInfo.CatchingUp || s.ValidatorInfo.VotingPower <= 0 || len(s.ValidatorInfo.Address) == 0 {
			return nil
		}
		labels := metric.WithAttributes(
			attribute.String("chain_id", chainID),
			attribute.String("consensus_address", s.ValidatorInfo.Address.String()),
			attribute.String("version", sdkversion.Version),
			attribute.String("commit", sdkversion.Commit),
		)
		buildInfo.Record(ctx, 1, labels)
		reportTime.Record(ctx, time.Now().Unix(), labels)
		exportCtx, cancel := context.WithTimeout(ctx, exportTimeout)
		defer cancel()
		var data metricdata.ResourceMetrics
		if err := reader.Collect(exportCtx, &data); err != nil {
			return err
		}
		return exporter.Export(exportCtx, &data)
	}

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := report(); err != nil && ctx.Err() == nil {
			logger.Warn("version report failed; will retry next interval", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
	return nil
}
