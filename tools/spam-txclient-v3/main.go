// Command spam-txclient-v3 is an async load generator for the v3 QueuedTxClient.
//
// Unlike the v1 spam_txclient (which broadcasts and confirms synchronously),
// this tool drives the non-blocking AddPayForBlob API: it keeps the internal
// queue saturated and awaits each TxHandle in the background, so it measures
// the real throughput ceiling of the async pipeline.
//
// It is designed to run on a talis validator (against localhost:9091 using the
// pre-funded "txsim" keyring account), but works standalone against any gRPC
// endpoint.
//
// Example:
//
//	go run ./tools/spam-txclient-v3 \
//	    -endpoint localhost:9091 -account txsim \
//	    -blob-size-kb 300 -duration 240s -queue-size 100
package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	v3 "github.com/celestiaorg/celestia-app/v9/pkg/user/v3"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config holds the load-test parameters, populated from command-line flags.
type Config struct {
	Endpoint       string
	BlobSizeKB     int
	Duration       time.Duration
	QueueSize      int
	Rate           int // attempted enqueues per second; 0 = saturate
	KeyringDir     string
	KeyringBackend string
	Account        string // keyring account that signs and pays
	OtelEndpoint   string // OTLP HTTP endpoint for metrics (empty = disabled)
}

func main() {
	cfg := parseFlags()

	if err := RunLoadTest(cfg); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		log.Fatalf("Load test failed: %v", err)
	}
}

func parseFlags() Config {
	var cfg Config
	flag.StringVar(&cfg.Endpoint, "endpoint", "localhost:9091", "gRPC endpoint of the node")
	flag.IntVar(&cfg.BlobSizeKB, "blob-size-kb", 300, "blob size in KiB")
	flag.DurationVar(&cfg.Duration, "duration", 240*time.Second, "total test duration")
	flag.IntVar(&cfg.QueueSize, "queue-size", 100, "QueuedTxClient async queue capacity")
	flag.IntVar(&cfg.Rate, "rate", 0, "attempted enqueues per second; 0 = saturate the queue")
	flag.StringVar(&cfg.KeyringDir, "keyring-dir", ".celestia-app", "keyring directory")
	flag.StringVar(&cfg.KeyringBackend, "keyring-backend", keyring.BackendTest, "keyring backend")
	flag.StringVar(&cfg.Account, "account", "txsim", "keyring account that signs and pays for txs")
	flag.StringVar(&cfg.OtelEndpoint, "otel-endpoint", "", "OpenTelemetry OTLP HTTP endpoint for metrics (e.g. http://host:4318); empty disables metrics")
	flag.Parse()
	return cfg
}

// metrics tracks counters across the submission and await goroutines.
type metrics struct {
	enqueued  atomic.Int64 // AddPayForBlob accepted into the queue
	queueFull atomic.Int64 // AddPayForBlob rejected because the queue was full
	addErr    atomic.Int64 // AddPayForBlob failed for other reasons
	confirmed atomic.Int64 // Await returned a committed tx (code 0)
	failedTx  atomic.Int64 // Await returned a tx with a non-zero code
	awaitErr  atomic.Int64 // Await returned an error
}

// settleGrace is how long, after the submission window closes, we keep the
// pipeline alive for in-flight txs to confirm before tearing the client down.
const settleGrace = 30 * time.Second

// instruments holds the OTel metric instruments. When metrics are disabled all
// fields are nil and the record* helpers are no-ops.
type instruments struct {
	enqueued  metric.Int64Counter
	confirmed metric.Int64Counter
	failedTx  metric.Int64Counter
	awaitErr  metric.Int64Counter
	queueFull metric.Int64Counter
	addErr    metric.Int64Counter
	latency   metric.Float64Histogram // enqueue -> confirm, milliseconds
}

func (in *instruments) add(ctx context.Context, c metric.Int64Counter) {
	if c != nil {
		c.Add(ctx, 1)
	}
}

func (in *instruments) recordLatency(ctx context.Context, ms float64) {
	if in.latency != nil {
		in.latency.Record(ctx, ms)
	}
}

// setupOTelMetrics wires a periodic OTLP HTTP metric exporter and returns the
// instruments plus a shutdown func. Mirrors fibre-txsim's setup.
func setupOTelMetrics(ctx context.Context, endpoint string) (*instruments, func(context.Context), error) {
	exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, nil, fmt.Errorf("creating OTLP metric exporter: %w", err)
	}

	hostname, _ := os.Hostname()
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("spam-txclient-v3"),
		semconv.ServiceInstanceID(hostname),
	))
	if err != nil {
		return nil, nil, fmt.Errorf("creating OTel resource: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(10*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	meter := mp.Meter("spam-txclient-v3")
	in := &instruments{}
	in.enqueued, _ = meter.Int64Counter("spam_v3.enqueued", metric.WithDescription("PFBs accepted into the async queue"))
	in.confirmed, _ = meter.Int64Counter("spam_v3.confirmed", metric.WithDescription("PFBs committed (code 0)"))
	in.failedTx, _ = meter.Int64Counter("spam_v3.failed_tx", metric.WithDescription("PFBs committed with a non-zero code"))
	in.awaitErr, _ = meter.Int64Counter("spam_v3.await_err", metric.WithDescription("Await returned an error"))
	in.queueFull, _ = meter.Int64Counter("spam_v3.queue_full", metric.WithDescription("AddPayForBlob rejected because the queue was full"))
	in.addErr, _ = meter.Int64Counter("spam_v3.add_err", metric.WithDescription("AddPayForBlob failed for other reasons"))
	// No WithUnit: the Prometheus exporter would otherwise append a unit suffix
	// (e.g. _milliseconds) on top of the _ms already in the name.
	in.latency, _ = meter.Float64Histogram("spam_v3.tx_latency_ms", metric.WithDescription("enqueue to confirm latency in milliseconds"))

	return in, func(ctx context.Context) {
		if err := mp.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "shutting down meter provider: %v\n", err)
		}
	}, nil
}

func RunLoadTest(cfg Config) error {
	log.Printf("Setting up v3 QueuedTxClient -> %s (blob=%dKiB, queue=%d, rate=%d, duration=%s)",
		cfg.Endpoint, cfg.BlobSizeKB, cfg.QueueSize, cfg.Rate, cfg.Duration)

	// lifeCtx governs the client and all Await calls; it outlives the
	// submission window by settleGrace so in-flight txs can confirm.
	lifeCtx, lifeCancel := context.WithTimeout(context.Background(), cfg.Duration+settleGrace)
	defer lifeCancel()

	// submitCtx bounds only the submission loop to the configured duration.
	submitCtx, submitCancel := context.WithTimeout(lifeCtx, cfg.Duration)
	defer submitCancel()

	// OTel metrics are optional; when no endpoint is set, in's instruments are
	// nil and all record* helpers are no-ops.
	in := &instruments{}
	if cfg.OtelEndpoint != "" {
		inst, shutdown, err := setupOTelMetrics(lifeCtx, cfg.OtelEndpoint)
		if err != nil {
			return fmt.Errorf("setup OTel metrics: %w", err)
		}
		in = inst
		defer shutdown(context.Background())
		log.Printf("OTel metrics enabled, exporting to %s", cfg.OtelEndpoint)
	}

	txClient, grpcConn, err := newQueuedTxClient(lifeCtx, cfg)
	if err != nil {
		return fmt.Errorf("failed to set up tx client: %w", err)
	}
	defer grpcConn.Close()
	defer txClient.Close()

	var m metrics
	var awaiters sync.WaitGroup

	// Reporter: periodic snapshot of the counters.
	stopReport := make(chan struct{})
	var reportWG sync.WaitGroup
	reportWG.Go(func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		start := time.Now()
		for {
			select {
			case <-stopReport:
				return
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				confirmed := m.confirmed.Load()
				fmt.Printf("[%4.0fs] enqueued=%d confirmed=%d (%.1f tx/s) failedTx=%d queueFull=%d awaitErr=%d addErr=%d\n",
					elapsed, m.enqueued.Load(), confirmed, float64(confirmed)/elapsed,
					m.failedTx.Load(), m.queueFull.Load(), m.awaitErr.Load(), m.addErr.Load())
			}
		}
	})

	// Submission loop: saturate the queue (or pace via ticker when -rate > 0).
	blobData := make([]byte, cfg.BlobSizeKB*1024)
	if _, err := cryptorand.Read(blobData); err != nil {
		return fmt.Errorf("generating blob data: %w", err)
	}

	var tick <-chan time.Time
	if cfg.Rate > 0 {
		t := time.NewTicker(time.Second / time.Duration(cfg.Rate))
		defer t.Stop()
		tick = t.C
	}

submitLoop:
	for {
		select {
		case <-submitCtx.Done():
			break submitLoop
		default:
		}

		if tick != nil {
			select {
			case <-submitCtx.Done():
				break submitLoop
			case <-tick:
			}
		}

		blob, err := randomBlob(blobData)
		if err != nil {
			return fmt.Errorf("building blob: %w", err)
		}

		// Await on lifeCtx (not submitCtx) so a tx enqueued near the end of
		// the window still gets the full settle grace to confirm.
		enqueuedAt := time.Now()
		handle, err := txClient.AddPayForBlob(submitCtx, []*share.Blob{blob})
		if err != nil {
			if strings.Contains(err.Error(), "queue is full") {
				m.queueFull.Add(1)
				in.add(lifeCtx, in.queueFull)
				// Queue saturated: yield briefly so the worker can drain.
				time.Sleep(time.Millisecond)
				continue
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break submitLoop
			}
			m.addErr.Add(1)
			in.add(lifeCtx, in.addErr)
			continue
		}
		m.enqueued.Add(1)
		in.add(lifeCtx, in.enqueued)

		awaiters.Go(func() {
			resp, err := handle.Await(lifeCtx)
			switch {
			case err != nil:
				m.awaitErr.Add(1)
				in.add(lifeCtx, in.awaitErr)
			case resp != nil && resp.Code == 0:
				m.confirmed.Add(1)
				in.add(lifeCtx, in.confirmed)
				in.recordLatency(lifeCtx, float64(time.Since(enqueuedAt).Milliseconds()))
			default:
				m.failedTx.Add(1)
				in.add(lifeCtx, in.failedTx)
			}
		})
	}

	log.Printf("Submission window elapsed; waiting up to %s for in-flight txs to settle...", settleGrace)
	// awaiters resolve via lifeCtx, which expires settleGrace after the
	// submission window — so this Wait is bounded even if txs never confirm.
	awaiters.Wait()

	close(stopReport)
	reportWG.Wait()

	fmt.Println("\n=== Load test complete ===")
	fmt.Printf("Enqueued:        %d\n", m.enqueued.Load())
	fmt.Printf("Confirmed (ok):  %d\n", m.confirmed.Load())
	fmt.Printf("Failed tx (code):%d\n", m.failedTx.Load())
	fmt.Printf("Await errors:    %d\n", m.awaitErr.Load())
	fmt.Printf("Queue full hits: %d\n", m.queueFull.Load())
	fmt.Printf("Add errors:      %d\n", m.addErr.Load())

	return nil
}

func randomBlob(data []byte) (*share.Blob, error) {
	return share.NewV0Blob(share.RandomBlobNamespace(), data)
}

func newQueuedTxClient(ctx context.Context, cfg Config) (*v3.QueuedTxClient, *grpc.ClientConn, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	kr, err := keyring.New(app.Name, cfg.KeyringBackend, expandHome(cfg.KeyringDir), nil, encCfg.Codec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize keyring: %w", err)
	}

	grpcConn, err := grpc.NewClient(
		cfg.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(math.MaxInt32),
			grpc.MaxCallRecvMsgSize(math.MaxInt32),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	v1Options := []user.Option{user.WithDefaultAccount(cfg.Account)}
	txClient, err := v3.SetupQueuedTxClient(ctx, kr, grpcConn, encCfg, v1Options, v3.WithQueueSize(cfg.QueueSize))
	if err != nil {
		grpcConn.Close()
		return nil, nil, fmt.Errorf("failed to create v3 tx client: %w", err)
	}

	return txClient, grpcConn, nil
}

// expandHome resolves a leading "~" to the user's home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
