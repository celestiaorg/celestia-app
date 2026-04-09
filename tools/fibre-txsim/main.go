package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const downloadDelay = 10 * time.Second

type config struct {
	grpcEndpoint string
	keyringDir   string
	keyPrefix    string
	blobSize     int
	concurrency  int
	interval     time.Duration
	duration     time.Duration
	otelEndpoint string
	download     bool
}

func main() {
	var cfg config
	flag.StringVar(&cfg.grpcEndpoint, "grpc-endpoint", "localhost:9091", "gRPC endpoint")
	flag.StringVar(&cfg.keyringDir, "keyring-dir", ".celestia-app", "keyring directory")
	flag.StringVar(&cfg.keyPrefix, "key-prefix", "fibre", "key name prefix in keyring (keys are named <prefix>-0, <prefix>-1, ...)")
	flag.IntVar(&cfg.blobSize, "blob-size", 1000000, "size of each blob in bytes")
	flag.IntVar(&cfg.concurrency, "concurrency", 1, "number of concurrent blob submissions (each gets its own account)")
	flag.DurationVar(&cfg.interval, "interval", 0, "delay between blob submissions per worker (0 = no delay)")
	flag.DurationVar(&cfg.duration, "duration", 0, "how long to run (0 = until killed)")
	flag.StringVar(&cfg.otelEndpoint, "otel-endpoint", "", "OpenTelemetry OTLP HTTP endpoint for metrics (e.g. http://host:4318)")
	flag.BoolVar(&cfg.download, "download", false, "enable download verification after each successful upload")
	chainID := flag.String("chain-id", "", "chain ID of the network (unused, accepted for compatibility)")
	flag.Parse()
	_ = chainID // accepted but unused

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// worker holds a per-account tx client and key name, sharing one fibre client.
type worker struct {
	fibreClient *fibre.Client
	txClient    *user.TxClient
	keyName     string
}

// downloadRequest is sent from upload workers to download workers after a successful upload.
type downloadRequest struct {
	blobID       fibre.BlobID
	originalData []byte
	fibreClient  *fibre.Client
	keyName      string
}

// stats tracks shared counters across all workers.
type stats struct {
	totalSent  atomic.Int64
	successes  atomic.Int64
	failures   atomic.Int64
	totalLatNs atomic.Int64

	dlSuccesses  atomic.Int64
	dlFailures   atomic.Int64
	dlTotalLatNs atomic.Int64
	dlVerified   atomic.Int64
}

func run(cfg config) error {
	if cfg.otelEndpoint != "" {
		metricsShutdown, err := setupOTelMetrics(context.Background(), cfg.otelEndpoint)
		if err != nil {
			return fmt.Errorf("setup OTel metrics: %w", err)
		}
		defer metricsShutdown(context.Background())

		traceShutdown, err := setupOTelTracing(context.Background(), cfg.otelEndpoint)
		if err != nil {
			return fmt.Errorf("setup OTel tracing: %w", err)
		}
		defer traceShutdown(context.Background())
		fmt.Printf("metrics and tracing enabled endpoint=%s\n", cfg.otelEndpoint)
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	kr, err := keyring.New(app.Name, keyring.BackendTest, cfg.keyringDir, nil, encCfg.Codec)
	if err != nil {
		return fmt.Errorf("failed to initialize keyring: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\nReceived interrupt, shutting down...")
		cancel()
	}()

	// Apply duration limit if set
	if cfg.duration > 0 {
		ctx, cancel = context.WithTimeout(ctx, cfg.duration)
		defer cancel()
	}

	// Create a single shared fibre client
	clientCfg := fibre.DefaultClientConfig()
	clientCfg.StateAddress = cfg.grpcEndpoint
	clientCfg.DefaultKeyName = fmt.Sprintf("%s-0", cfg.keyPrefix)

	sharedFibreClient, err := fibre.NewClient(kr, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to create shared fibre client: %w", err)
	}

	if err := sharedFibreClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start shared fibre client: %w", err)
	}

	defer func() {
		// Use a fresh context so Stop can wait for in-flight operations to complete
		// even after the parent ctx has been cancelled by signal/timeout.
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := sharedFibreClient.Stop(stopCtx); err != nil {
			fmt.Fprintf(os.Stderr, "stopping shared fibre client: %v\n", err)
		}
	}()

	// Create one worker per concurrent slot, each with its own account
	workers := make([]worker, cfg.concurrency)
	for i := range cfg.concurrency {
		keyName := fmt.Sprintf("%s-%d", cfg.keyPrefix, i)

		grpcConn, err := grpc.NewClient(
			cfg.grpcEndpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(
				grpc.MaxCallSendMsgSize(math.MaxInt32),
				grpc.MaxCallRecvMsgSize(math.MaxInt32),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to create gRPC connection for worker %d: %w", i, err)
		}
		defer grpcConn.Close()

		txClient, err := user.SetupTxClient(ctx, kr, grpcConn, encCfg, user.WithDefaultAccount(keyName))
		if err != nil {
			return fmt.Errorf("failed to set up tx client for worker %d (%s): %w", i, keyName, err)
		}

		workers[i] = worker{
			fibreClient: sharedFibreClient,
			txClient:    txClient,
			keyName:     keyName,
		}
		fmt.Printf("Worker %d initialized with key %s\n", i, keyName)
	}

	st := &stats{}
	startTime := time.Now()

	fmt.Printf("\nStarting fibre blob spam with %d workers...\n", cfg.concurrency)

	// Download channel: upload workers send requests, download workers process them.
	const downloadWorkers = 4
	var dlCh chan downloadRequest
	if cfg.download {
		dlCh = make(chan downloadRequest, downloadWorkers*4)
	}

	var uploadWg sync.WaitGroup
	var dlWg sync.WaitGroup

	// Spawn download workers
	if cfg.download {
		for range downloadWorkers {
			dlWg.Go(func() {
				downloadWorkerLoop(ctx, dlCh, st)
			})
		}
	}

	// Launch upload workers
	for _, w := range workers {
		uploadWg.Go(func() {
			for ctx.Err() == nil {
				submitBlob(ctx, w, cfg.blobSize, st, dlCh)
				if cfg.interval > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(cfg.interval):
					}
				}
			}
		})
	}

	// Close the download channel only after all upload workers have finished,
	// guaranteeing no goroutine will send to a closed channel.
	if cfg.download {
		go func() {
			uploadWg.Wait()
			close(dlCh)
		}()
	}

	uploadWg.Wait()
	dlWg.Wait()

	elapsed := time.Since(startTime)
	s := st.successes.Load()
	f := st.failures.Load()
	var avgLat time.Duration
	if s > 0 {
		avgLat = time.Duration(st.totalLatNs.Load() / s)
	}

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Duration:   %s\n", elapsed.Truncate(time.Second))
	fmt.Println()
	fmt.Println("Uploads:")
	fmt.Printf("  Total sent: %d\n", st.totalSent.Load())
	fmt.Printf("  Successes:  %d\n", s)
	fmt.Printf("  Failures:   %d\n", f)
	fmt.Printf("  Avg latency (success): %s\n", avgLat)

	if cfg.download {
		ds := st.dlSuccesses.Load()
		df := st.dlFailures.Load()
		dv := st.dlVerified.Load()
		var avgDlLat time.Duration
		if ds > 0 {
			avgDlLat = time.Duration(st.dlTotalLatNs.Load() / ds)
		}

		fmt.Println()
		fmt.Println("Downloads:")
		fmt.Printf("  Successes:  %d\n", ds)
		fmt.Printf("  Failures:   %d\n", df)
		fmt.Printf("  Verified:   %d\n", dv)
		fmt.Printf("  Avg latency (success): %s\n", avgDlLat)
	}

	return nil
}

func setupOTelMetrics(ctx context.Context, endpoint string) (func(context.Context), error) {
	exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("creating OTLP metric exporter: %w", err)
	}

	hostname, _ := os.Hostname()
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("fibre-txsim"),
		semconv.ServiceInstanceID(hostname),
	))
	if err != nil {
		return nil, fmt.Errorf("creating OTel resource: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(10*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) {
		if err := mp.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "shutting down meter provider: %v\n", err)
		}
	}, nil
}

func setupOTelTracing(ctx context.Context, endpoint string) (func(context.Context), error) {
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	hostname, _ := os.Hostname()
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("fibre-txsim"),
		semconv.ServiceInstanceID(hostname),
	))
	if err != nil {
		return nil, fmt.Errorf("creating OTel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return func(ctx context.Context) {
		if err := tp.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "shutting down tracer provider: %v\n", err)
		}
	}, nil
}

func submitBlob(ctx context.Context, w worker, blobSize int, st *stats, dlCh chan<- downloadRequest) {
	// Generate random namespace
	nsID := make([]byte, share.NamespaceVersionZeroIDSize)
	if _, err := rand.Read(nsID); err != nil {
		fmt.Printf("[%s] error generating namespace: %v\n", w.keyName, err)
		st.failures.Add(1)
		st.totalSent.Add(1)
		return
	}
	id := make([]byte, 0, share.NamespaceIDSize)
	id = append(id, share.NamespaceVersionZeroPrefix...)
	id = append(id, nsID...)
	ns, err := share.NewNamespace(share.NamespaceVersionZero, id)
	if err != nil {
		fmt.Printf("[%s] error creating namespace: %v\n", w.keyName, err)
		st.failures.Add(1)
		st.totalSent.Add(1)
		return
	}

	// Generate random blob data
	data := make([]byte, blobSize)
	if _, err := rand.Read(data); err != nil {
		fmt.Printf("[%s] error generating blob data: %v\n", w.keyName, err)
		st.failures.Add(1)
		st.totalSent.Add(1)
		return
	}

	t := time.Now()
	result, err := fibre.Put(ctx, w.fibreClient, w.txClient, ns, data)
	lat := time.Since(t)

	st.totalSent.Add(1)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		st.failures.Add(1)
		fmt.Printf("[%s] upload error: %v (latency=%s)\n", w.keyName, err, lat)
		return
	}

	st.successes.Add(1)
	st.totalLatNs.Add(lat.Nanoseconds())
	fmt.Printf("[%s] upload: height=%d tx=%s latency=%s\n", w.keyName, result.Height, result.TxHash, lat)

	// Send download request (non-blocking) to download workers
	if dlCh != nil {
		select {
		case dlCh <- downloadRequest{
			blobID:       result.BlobID,
			originalData: data,
			fibreClient:  w.fibreClient,
			keyName:      w.keyName,
		}:
		default:
			// Channel full, skip this download to avoid blocking uploads
		}
	}
}

func downloadWorkerLoop(ctx context.Context, dlCh <-chan downloadRequest, st *stats) {
	for req := range dlCh {
		downloadBlob(ctx, &req, st)
	}
}

func downloadBlob(ctx context.Context, req *downloadRequest, st *stats) {
	// Wait before downloading to allow the blob to propagate
	select {
	case <-time.After(downloadDelay):
	case <-ctx.Done():
		// Context cancelled during delay, still attempt download to drain
	}

	fmt.Printf("[%s] download starting after %s delay: blob_id=%s\n",
		req.keyName, downloadDelay, req.blobID)

	t := time.Now()
	// Use a separate context for download so we can still download after the main ctx is cancelled
	dlCtx, dlCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer dlCancel()

	blob, err := req.fibreClient.Download(dlCtx, req.blobID)
	if err != nil {
		st.dlFailures.Add(1)
		fmt.Printf("[%s] download error: blob_id=%s %v (latency=%s)\n",
			req.keyName, req.blobID, err, time.Since(t))
		return
	}

	lat := time.Since(t)
	verified := bytes.Equal(blob.Data(), req.originalData)
	st.dlSuccesses.Add(1)
	st.dlTotalLatNs.Add(lat.Nanoseconds())
	if verified {
		st.dlVerified.Add(1)
	}
	fmt.Printf("[%s] download: blob_id=%s latency=%s verified=%t\n",
		req.keyName, req.blobID, lat, verified)
}
