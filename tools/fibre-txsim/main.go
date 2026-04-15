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
	"github.com/celestiaorg/celestia-app/v9/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	fibretypes "github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/grafana/pyroscope-go"
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
	grpcEndpoint      string
	keyringDir        string
	keyPrefix         string
	blobSize          int
	concurrency       int
	interval          time.Duration
	duration          time.Duration
	otelEndpoint      string
	download          bool
	uploadOnly        bool
	pyroscopeEndpoint string
	pyroscopeUser     string
	pyroscopePass     string
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
	flag.BoolVar(&cfg.uploadOnly, "upload-only", false, "skip PFF transaction — only upload shards to validators without on-chain confirmation")
	flag.StringVar(&cfg.pyroscopeEndpoint, "pyroscope-endpoint", "", "Pyroscope endpoint for continuous profiling (e.g. http://host:4040)")
	flag.StringVar(&cfg.pyroscopeUser, "pyroscope-basic-auth-user", "", "Pyroscope basic auth username")
	flag.StringVar(&cfg.pyroscopePass, "pyroscope-basic-auth-password", "", "Pyroscope basic auth password")
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
	grpcConn    *grpc.ClientConn
	keyName     string
}

// downloadRequest is sent from upload workers to download workers after a successful upload.
type downloadRequest struct {
	blobID       fibre.BlobID
	originalData []byte
	fibreClient  *fibre.Client
	keyName      string
}

// confirmRequest is sent from upload workers to confirmation workers after broadcasting a PFF tx.
type confirmRequest struct {
	grpcConn *grpc.ClientConn
	txHash   string
	keyName  string
	startT   time.Time
}

// stats tracks shared counters across all workers.
type stats struct {
	totalSent  atomic.Int64
	successes  atomic.Int64
	failures   atomic.Int64
	totalLatNs atomic.Int64

	// upload-only / encode+upload+broadcast latency (async TX mode)
	uploadLatNs atomic.Int64
	uploadCount atomic.Int64

	// async confirmation tracking
	confirmSuccesses atomic.Int64
	confirmFailures  atomic.Int64
	confirmLatNs     atomic.Int64

	dlSuccesses  atomic.Int64
	dlFailures   atomic.Int64
	dlTotalLatNs atomic.Int64
	dlVerified   atomic.Int64
}

func run(cfg config) error {
	if cfg.concurrency <= 0 {
		return fmt.Errorf("--concurrency must be >= 1, got %d", cfg.concurrency)
	}

	if cfg.pyroscopeEndpoint != "" {
		stopPyroscope, err := setupPyroscope(cfg.pyroscopeEndpoint, cfg.pyroscopeUser, cfg.pyroscopePass)
		if err != nil {
			return fmt.Errorf("setup Pyroscope: %w", err)
		}
		defer stopPyroscope()
		fmt.Printf("profiling enabled endpoint=%s\n", cfg.pyroscopeEndpoint)
	}

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
			grpcConn:    grpcConn,
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

	// Confirmation channel: upload workers send requests, confirm workers poll for inclusion.
	const confirmWorkers = 8
	var confirmCh chan confirmRequest
	if !cfg.uploadOnly {
		confirmCh = make(chan confirmRequest, cfg.concurrency*4)
	}

	var uploadWg sync.WaitGroup
	var dlWg sync.WaitGroup
	var confirmWg sync.WaitGroup

	// Spawn download workers
	if cfg.download {
		for range downloadWorkers {
			dlWg.Go(func() {
				downloadWorkerLoop(ctx, dlCh, st)
			})
		}
	}

	// Spawn confirmation workers. Each confirmRequest carries its own gRPC
	// connection and polls TxStatus directly, avoiding TxClient.ConfirmTx which
	// has a data race when called concurrently with BroadcastTx.
	if confirmCh != nil {
		for range confirmWorkers {
			confirmWg.Go(func() {
				confirmWorkerLoop(ctx, confirmCh, st)
			})
		}
	}

	// Launch upload workers
	for _, w := range workers {
		uploadWg.Go(func() {
			for ctx.Err() == nil {
				submitBlob(ctx, w, cfg.blobSize, cfg.uploadOnly, st, dlCh, confirmCh)
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

	// Close channels only after all upload workers have finished,
	// guaranteeing no goroutine will send to a closed channel.
	go func() {
		uploadWg.Wait()
		if dlCh != nil {
			close(dlCh)
		}
		if confirmCh != nil {
			close(confirmCh)
		}
	}()

	uploadWg.Wait()
	confirmWg.Wait()
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

	if uc := st.uploadCount.Load(); uc > 0 {
		avgUploadLat := time.Duration(st.uploadLatNs.Load() / uc)
		fmt.Printf("  Avg upload latency (encode+upload+broadcast): %s\n", avgUploadLat)
	}

	if confirmCh != nil {
		cs := st.confirmSuccesses.Load()
		cf := st.confirmFailures.Load()
		var avgConfirmLat time.Duration
		if cs > 0 {
			avgConfirmLat = time.Duration(st.confirmLatNs.Load() / cs)
		}
		fmt.Println()
		fmt.Println("Confirmations:")
		fmt.Printf("  Successes:  %d\n", cs)
		fmt.Printf("  Failures:   %d\n", cf)
		fmt.Printf("  Avg latency (success): %s\n", avgConfirmLat)
	}

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

func setupPyroscope(endpoint, user, pass string) (func(), error) {
	hostname, _ := os.Hostname()
	profiler, err := pyroscope.Start(pyroscope.Config{
		ApplicationName:   "fibre-txsim",
		ServerAddress:     endpoint,
		BasicAuthUser:     user,
		BasicAuthPassword: pass,
		Tags:              map[string]string{"hostname": hostname},
	})
	if err != nil {
		return nil, fmt.Errorf("starting Pyroscope profiler: %w", err)
	}
	return func() {
		if err := profiler.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "stopping Pyroscope profiler: %v\n", err)
		}
	}, nil
}

func submitBlob(ctx context.Context, w worker, blobSize int, uploadOnly bool, st *stats, dlCh chan<- downloadRequest, confirmCh chan<- confirmRequest) {
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

	st.totalSent.Add(1)
	t := time.Now()

	if uploadOnly {
		blob, err := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
		if err != nil {
			st.failures.Add(1)
			fmt.Printf("[%s] blob encode error: %v\n", w.keyName, err)
			return
		}
		_, err = w.fibreClient.Upload(ctx, ns, blob, fibre.WithKeyName(w.keyName))
		lat := time.Since(t)
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
		fmt.Printf("[%s] upload-only: latency=%s\n", w.keyName, lat)
		return
	}

	// Async TX mode: encode, upload, broadcast, then hand off confirmation to background workers.
	blob, err := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
	if err != nil {
		st.failures.Add(1)
		fmt.Printf("[%s] blob encode error: %v\n", w.keyName, err)
		return
	}

	signedPromise, err := w.fibreClient.Upload(ctx, ns, blob, fibre.WithKeyName(w.keyName))
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		st.failures.Add(1)
		fmt.Printf("[%s] upload error: %v\n", w.keyName, err)
		return
	}

	promiseProto, err := signedPromise.ToProto()
	if err != nil {
		st.failures.Add(1)
		fmt.Printf("[%s] promise proto error: %v\n", w.keyName, err)
		return
	}

	msg := &fibretypes.MsgPayForFibre{
		Signer:              w.txClient.DefaultAddress().String(),
		PaymentPromise:      *promiseProto,
		ValidatorSignatures: signedPromise.ValidatorSignatures,
	}

	broadcastResp, err := w.txClient.BroadcastTx(ctx, []sdk.Msg{msg})
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		st.failures.Add(1)
		fmt.Printf("[%s] broadcast error: %v\n", w.keyName, err)
		return
	}

	uploadLat := time.Since(t)
	st.successes.Add(1)
	st.totalLatNs.Add(uploadLat.Nanoseconds())
	st.uploadLatNs.Add(uploadLat.Nanoseconds())
	st.uploadCount.Add(1)
	fmt.Printf("[%s] broadcast: tx=%s upload_latency=%s\n", w.keyName, broadcastResp.TxHash, uploadLat)

	// Hand off confirmation to background workers (non-blocking).
	if confirmCh != nil {
		select {
		case confirmCh <- confirmRequest{
			grpcConn: w.grpcConn,
			txHash:   broadcastResp.TxHash,
			keyName:  w.keyName,
			startT:   t,
		}:
		default:
			// Channel full, skip confirmation tracking to avoid blocking uploads.
		}
	}

	// Send download request (non-blocking) to download workers.
	if dlCh != nil {
		select {
		case dlCh <- downloadRequest{
			blobID:       blob.ID(),
			originalData: data,
			fibreClient:  w.fibreClient,
			keyName:      w.keyName,
		}:
		default:
			// Channel full, skip this download to avoid blocking uploads.
		}
	}
}

// confirmWorkerLoop polls TxStatus for each broadcast tx without using TxClient.ConfirmTx.
// This avoids a data race: ConfirmTx modifies the signer's sequence on rejection/eviction
// without holding the client mutex, which races with concurrent BroadcastTx calls from
// the upload workers. Since fibre-txsim doesn't need sequence recovery or tx resubmission,
// a simple status poll is sufficient.
func confirmWorkerLoop(ctx context.Context, ch <-chan confirmRequest, st *stats) {
	for req := range ch {
		height, err := pollTxStatus(ctx, req.grpcConn, req.txHash, 2*time.Minute)
		if err != nil {
			st.confirmFailures.Add(1)
			fmt.Printf("[%s] confirm error: tx=%s %v\n", req.keyName, req.txHash, err)
			continue
		}
		lat := time.Since(req.startT)
		st.confirmSuccesses.Add(1)
		st.confirmLatNs.Add(lat.Nanoseconds())
		fmt.Printf("[%s] confirmed: tx=%s height=%d total_latency=%s\n",
			req.keyName, req.txHash, height, lat)
	}
}

// pollTxStatus polls TxStatus directly via gRPC without touching the TxClient's
// signer or tx tracker. Returns the height on commit, or an error on rejection/timeout.
func pollTxStatus(ctx context.Context, conn *grpc.ClientConn, txHash string, timeout time.Duration) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	txClient := tx.NewTxClient(conn)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		resp, err := txClient.TxStatus(ctx, &tx.TxStatusRequest{TxId: txHash})
		if err != nil {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			// Transient gRPC error, keep polling
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-ticker.C:
				continue
			}
		}

		switch resp.Status {
		case "COMMITTED":
			if resp.ExecutionCode != 0 {
				return 0, fmt.Errorf("tx %s execution error (code %d): %s", txHash, resp.ExecutionCode, resp.Error)
			}
			return resp.Height, nil
		case "REJECTED":
			return 0, fmt.Errorf("tx %s rejected: %s", txHash, resp.Error)
		case "EVICTED":
			return 0, fmt.Errorf("tx %s evicted", txHash)
		default:
			// PENDING or UNKNOWN, keep polling
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
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
