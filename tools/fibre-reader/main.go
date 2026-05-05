// fibre-reader is a third-party reading simulator for the fibre protocol.
//
// It subscribes to a Celestia validator's RPC for NewBlock events,
// scans each block for MsgPayForFibre transactions, extracts BlobIDs,
// applies hash-modulo sharding (commitment[0:8] % reader_count == reader_index)
// to determine ownership, and concurrently downloads owned blobs via fibre.Client.
//
// Mode: trail-only. Does NOT catch up to historical heights — only blobs
// posted after subscription begins are observed. To distribute load across
// a cluster, run N reader instances with --reader-index 0..N-1 --reader-count N;
// commitments are uniformly hashed so each blob is downloaded exactly once.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/celestiaorg/celestia-app/v9/fibre/state"
	"github.com/celestiaorg/celestia-app/v9/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/grafana/pyroscope-go"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

type config struct {
	rpcEndpoint         string
	grpcEndpoint        string
	keyringDir          string
	keyName             string
	readerIndex         int
	readerCount         int
	downloadConcurrency int
	downloadTimeout     time.Duration
	startupTimeout      time.Duration
	duration            time.Duration
	otelEndpoint        string
	pyroscopeEndpoint   string
	pyroscopeUser       string
	pyroscopePass       string
}

type stats struct {
	blobsSeen            atomic.Int64
	blobsOwned           atomic.Int64
	blobsSkipped         atomic.Int64
	blobsDropped         atomic.Int64
	downloadsSuccess     atomic.Int64
	downloadsFailed      atomic.Int64
	commitmentMismatches atomic.Int64
	downloadedBytes      atomic.Int64
	dlTotalLatNs         atomic.Int64
	e2eTotalLatNs        atomic.Int64
	inclusionLatNs       atomic.Int64
	queueWaitNs          atomic.Int64
}

type readerMetrics struct {
	blobsSeen            metric.Int64Counter
	blobsOwned           metric.Int64Counter
	blobsSkipped         metric.Int64Counter
	downloadsSuccess     metric.Int64Counter
	downloadsFailed      metric.Int64Counter
	commitmentMismatches metric.Int64Counter
	downloadedBytes      metric.Int64Counter
	downloadLatency      metric.Float64Histogram
	e2eLatency           metric.Float64Histogram
	inclusionLatency     metric.Float64Histogram
	blockProcessLatency  metric.Float64Histogram
	queueWaitLatency     metric.Float64Histogram
}

type downloadRequest struct {
	blobID            fibre.BlobID
	commitment        fibre.Commitment
	height            int64
	creationTimestamp time.Time
	blockTime         time.Time
	dataSize          uint32
	queuedAt          time.Time
}

func main() {
	var cfg config
	flag.StringVar(&cfg.rpcEndpoint, "rpc-endpoint", "tcp://localhost:26657", "cometbft RPC endpoint")
	flag.StringVar(&cfg.grpcEndpoint, "grpc-endpoint", "localhost:9091", "celestia-app gRPC endpoint for fibre client state")
	flag.StringVar(&cfg.keyringDir, "keyring-dir", ".celestia-app", "keyring directory")
	flag.StringVar(&cfg.keyName, "key-name", "fibre-0", "key name in keyring (used to satisfy fibre.NewClient existence check)")
	flag.IntVar(&cfg.readerIndex, "reader-index", -1, "this reader's index in [0, reader-count)")
	flag.IntVar(&cfg.readerCount, "reader-count", 0, "total number of reader instances (>=1)")
	flag.IntVar(&cfg.downloadConcurrency, "download-concurrency", 32, "max concurrent in-flight downloads (semaphore-bounded; goroutine spawned per blob)")
	flag.DurationVar(&cfg.downloadTimeout, "download-timeout", 2*time.Minute, "per-download timeout")
	flag.DurationVar(&cfg.startupTimeout, "startup-timeout", 5*time.Minute, "how long to retry connecting to the validator's gRPC + cometbft RPC at startup before giving up (handles validators not yet ready / brief restarts)")
	flag.DurationVar(&cfg.duration, "duration", 0, "how long to run (0 = until killed)")
	flag.StringVar(&cfg.otelEndpoint, "otel-endpoint", "", "OpenTelemetry OTLP HTTP endpoint for metrics + tracing (e.g. http://host:4318)")
	flag.StringVar(&cfg.pyroscopeEndpoint, "pyroscope-endpoint", "", "Pyroscope endpoint for continuous profiling (e.g. http://host:4040)")
	flag.StringVar(&cfg.pyroscopeUser, "pyroscope-basic-auth-user", "", "Pyroscope basic auth username")
	flag.StringVar(&cfg.pyroscopePass, "pyroscope-basic-auth-password", "", "Pyroscope basic auth password")
	chainID := flag.String("chain-id", "", "chain ID of the network (unused, accepted for compatibility)")
	flag.Parse()
	_ = chainID

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(cfg config) error {
	if cfg.readerCount < 1 {
		return fmt.Errorf("--reader-count must be >= 1, got %d", cfg.readerCount)
	}
	if cfg.readerIndex < 0 || cfg.readerIndex >= cfg.readerCount {
		return fmt.Errorf("--reader-index must be in [0, %d), got %d", cfg.readerCount, cfg.readerIndex)
	}
	if cfg.downloadConcurrency <= 0 {
		return fmt.Errorf("--download-concurrency must be >= 1, got %d", cfg.downloadConcurrency)
	}

	if cfg.pyroscopeEndpoint != "" {
		stopPyroscope, err := setupPyroscope(cfg.pyroscopeEndpoint, cfg.pyroscopeUser, cfg.pyroscopePass)
		if err != nil {
			return fmt.Errorf("setup Pyroscope: %w", err)
		}
		defer stopPyroscope()
		fmt.Printf("[reader-%d] profiling enabled endpoint=%s\n", cfg.readerIndex, cfg.pyroscopeEndpoint)
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
		fmt.Printf("[reader-%d] metrics and tracing enabled endpoint=%s\n", cfg.readerIndex, cfg.otelEndpoint)
	}

	rm, err := newReaderMetrics()
	if err != nil {
		return fmt.Errorf("creating reader metrics: %w", err)
	}
	tracer := otel.Tracer("fibre-reader")

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	kr, err := keyring.New(app.Name, keyring.BackendTest, cfg.keyringDir, nil, encCfg.Codec)
	if err != nil {
		return fmt.Errorf("failed to initialize keyring: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Printf("\n[reader-%d] received interrupt, shutting down...\n", cfg.readerIndex)
		cancel()
	}()

	if cfg.duration > 0 {
		ctx, cancel = context.WithTimeout(ctx, cfg.duration)
		defer cancel()
	}

	clientCfg := fibre.DefaultClientConfig()
	clientCfg.StateAddress = cfg.grpcEndpoint
	clientCfg.DefaultKeyName = cfg.keyName
	if err := clientCfg.Validate(); err != nil {
		return fmt.Errorf("invalid fibre client config: %w", err)
	}
	clientCfg.StateClientFn = state.WithCachedValset(clientCfg.StateClientFn, 30*time.Second)

	fibreClient, err := fibre.NewClient(kr, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to create fibre client: %w", err)
	}
	if err := retryStartup(ctx, cfg, "fibre-client", func(ctx context.Context) error {
		return fibreClient.Start(ctx)
	}); err != nil {
		return fmt.Errorf("failed to start fibre client: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := fibreClient.Stop(stopCtx); err != nil {
			fmt.Fprintf(os.Stderr, "[reader-%d] stopping fibre client: %v\n", cfg.readerIndex, err)
		}
	}()

	rpcClient, err := http.New(cfg.rpcEndpoint, "/websocket")
	if err != nil {
		return fmt.Errorf("creating rpc client: %w", err)
	}
	if err := rpcClient.Start(); err != nil {
		return fmt.Errorf("starting rpc client: %w", err)
	}
	defer func() {
		if err := rpcClient.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "[reader-%d] stopping rpc client: %v\n", cfg.readerIndex, err)
		}
	}()

	subID := fmt.Sprintf("fibre-reader-%d", cfg.readerIndex)
	var sub <-chan coretypes.ResultEvent
	if err := retryStartup(ctx, cfg, "rpc-subscribe", func(ctx context.Context) error {
		var subErr error
		sub, subErr = rpcClient.Subscribe(ctx, subID, "tm.event='NewBlock'")
		return subErr
	}); err != nil {
		return fmt.Errorf("subscribing to NewBlock: %w", err)
	}
	defer func() {
		unsubCtx, unsubCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer unsubCancel()
		if err := rpcClient.UnsubscribeAll(unsubCtx, subID); err != nil {
			fmt.Fprintf(os.Stderr, "[reader-%d] unsubscribing: %v\n", cfg.readerIndex, err)
		}
	}()

	// Semaphore + goroutine-per-blob: every owned blob spawns its own download
	// goroutine and acquires a slot from sem. No central queue, no drops; all
	// owned blobs from a single block can run in parallel up to the semaphore
	// bound. Goroutines waiting on a full sem are tiny (~few KB each), so brief
	// bursts past the bound just queue at the sem rather than dropping data.
	sem := make(chan struct{}, cfg.downloadConcurrency)
	st := &stats{}
	var dlWg sync.WaitGroup

	startTime := time.Now()
	fmt.Printf("[reader-%d] reader-count=%d download-concurrency=%d trailing %s...\n",
		cfg.readerIndex, cfg.readerCount, cfg.downloadConcurrency, cfg.rpcEndpoint)

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case result, ok := <-sub:
			if !ok {
				break loop
			}
			ev, ok := result.Data.(cmttypes.EventDataNewBlock)
			if !ok {
				fmt.Fprintf(os.Stderr, "[reader-%d] unexpected event data type: %T\n", cfg.readerIndex, result.Data)
				continue
			}
			processBlock(ctx, ev.Block, cfg, sem, &dlWg, fibreClient, st, rm, tracer)
		}
	}

	dlWg.Wait()

	elapsed := time.Since(startTime)
	printSummary(cfg, st, elapsed)

	return nil
}

func processBlock(
	ctx context.Context,
	block *cmttypes.Block,
	cfg config,
	sem chan struct{},
	dlWg *sync.WaitGroup,
	fibreClient *fibre.Client,
	st *stats,
	rm *readerMetrics,
	tracer trace.Tracer,
) {
	processStart := time.Now()

	ctx, span := tracer.Start(ctx, "fibre_reader.block.process",
		trace.WithAttributes(
			attribute.Int64("block.height", block.Height),
			attribute.Int("block.tx_count", len(block.Data.Txs)),
		),
	)
	defer span.End()

	txs, err := testnode.DecodeBlockData(block.Data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[reader-%d] decoding block %d: %v\n", cfg.readerIndex, block.Height, err)
		span.RecordError(err)
		return
	}

	fibreMsgCount := 0
	for _, tx := range txs {
		for _, msg := range tx.GetMsgs() {
			pff, ok := msg.(*fibretypes.MsgPayForFibre)
			if !ok {
				continue
			}
			fibreMsgCount++
			handlePayForFibre(ctx, pff, block, cfg, sem, dlWg, fibreClient, st, rm, tracer)
		}
	}

	span.SetAttributes(attribute.Int("block.fibre_msg_count", fibreMsgCount))

	procLat := time.Since(processStart)
	if rm != nil {
		rm.blockProcessLatency.Record(ctx, float64(procLat.Milliseconds()))
	}
}

func handlePayForFibre(
	ctx context.Context,
	msg *fibretypes.MsgPayForFibre,
	block *cmttypes.Block,
	cfg config,
	sem chan struct{},
	dlWg *sync.WaitGroup,
	fibreClient *fibre.Client,
	st *stats,
	rm *readerMetrics,
	tracer trace.Tracer,
) {
	promise := msg.PaymentPromise

	if len(promise.Commitment) != fibre.CommitmentSize {
		fmt.Fprintf(os.Stderr, "[reader-%d] invalid commitment length=%d at height=%d\n",
			cfg.readerIndex, len(promise.Commitment), block.Height)
		return
	}

	var commitment fibre.Commitment
	copy(commitment[:], promise.Commitment)
	blobID := fibre.NewBlobID(uint8(promise.BlobVersion), commitment)

	st.blobsSeen.Add(1)
	if rm != nil {
		rm.blobsSeen.Add(context.Background(), 1)
	}

	if !owns(commitment, cfg.readerCount, cfg.readerIndex) {
		st.blobsSkipped.Add(1)
		if rm != nil {
			rm.blobsSkipped.Add(context.Background(), 1)
		}
		return
	}

	st.blobsOwned.Add(1)
	if rm != nil {
		rm.blobsOwned.Add(context.Background(), 1)
	}

	req := downloadRequest{
		blobID:            blobID,
		commitment:        commitment,
		height:            promise.Height,
		creationTimestamp: promise.CreationTimestamp,
		blockTime:         block.Header.Time,
		dataSize:          promise.BlobSize,
		queuedAt:          time.Now(),
	}

	dlWg.Add(1)
	go func() {
		defer dlWg.Done()
		// Acquire a slot. Blocks until one is free or ctx cancels — backpressure
		// instead of dropping. Multiple owned blobs in a single block all reach
		// here concurrently and run in parallel up to cfg.downloadConcurrency.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-sem }()
		downloadOne(ctx, req, fibreClient, cfg, st, rm, tracer)
	}()
}

// owns returns true when this reader instance is responsible for the given commitment
// under hash-modulo sharding. Commitments are SHA-derived so a uint64 prefix is uniform.
func owns(commitment fibre.Commitment, count, index int) bool {
	return binary.BigEndian.Uint64(commitment[:8])%uint64(count) == uint64(index)
}

func downloadOne(
	ctx context.Context,
	req downloadRequest,
	fibreClient *fibre.Client,
	cfg config,
	st *stats,
	rm *readerMetrics,
	tracer trace.Tracer,
) {
	dlCtx, dlCancel := context.WithTimeout(ctx, cfg.downloadTimeout)
	defer dlCancel()

	dlCtx, span := tracer.Start(dlCtx, "fibre_reader.blob.download",
		trace.WithAttributes(
			attribute.String("blob.commitment", hex.EncodeToString(req.commitment[:])),
			attribute.Int64("blob.height", req.height),
			attribute.Int("reader.index", cfg.readerIndex),
		),
	)
	defer span.End()

	// queue_wait = time between block scan creating the request and this
	// goroutine acquiring its semaphore slot — surfaces saturation when the
	// bound is hit.
	queueWait := time.Since(req.queuedAt)
	st.queueWaitNs.Add(queueWait.Nanoseconds())
	if rm != nil {
		rm.queueWaitLatency.Record(context.Background(), float64(queueWait.Milliseconds()))
	}
	span.AddEvent("download.started", trace.WithAttributes(
		attribute.Int64("queue_wait_ms", queueWait.Milliseconds()),
	))

	start := time.Now()
	blob, err := fibreClient.Download(dlCtx, req.blobID, fibre.WithHeight(uint64(req.height)))
	dlLat := time.Since(start)

	if err != nil {
		st.downloadsFailed.Add(1)
		if errors.Is(err, fibre.ErrBlobCommitmentMismatch) {
			st.commitmentMismatches.Add(1)
			if rm != nil {
				rm.commitmentMismatches.Add(context.Background(), 1)
			}
		}
		if rm != nil {
			rm.downloadsFailed.Add(context.Background(), 1)
		}
		span.RecordError(err)
		fmt.Fprintf(os.Stderr, "[reader-%d] download failed commitment=%s height=%d latency=%s queue_wait=%s err=%v\n",
			cfg.readerIndex, req.commitment, req.height, dlLat, queueWait, err)
		return
	}

	now := time.Now()
	e2eLat := now.Sub(req.creationTimestamp)
	inclusionLat := now.Sub(req.blockTime)
	bytesDl := int64(blob.DataSize())

	st.downloadsSuccess.Add(1)
	st.downloadedBytes.Add(bytesDl)
	st.dlTotalLatNs.Add(dlLat.Nanoseconds())
	st.e2eTotalLatNs.Add(e2eLat.Nanoseconds())
	st.inclusionLatNs.Add(inclusionLat.Nanoseconds())

	if rm != nil {
		ctxBg := context.Background()
		rm.downloadsSuccess.Add(ctxBg, 1)
		rm.downloadedBytes.Add(ctxBg, bytesDl)
		rm.downloadLatency.Record(ctxBg, float64(dlLat.Milliseconds()))
		rm.e2eLatency.Record(ctxBg, float64(e2eLat.Milliseconds()))
		rm.inclusionLatency.Record(ctxBg, float64(inclusionLat.Milliseconds()))
	}

	span.SetAttributes(attribute.Int("blob.size", blob.DataSize()))
	fmt.Printf("[reader-%d] download ok commitment=%s height=%d size=%d dl_latency=%s queue_wait=%s e2e_latency=%s inclusion_latency=%s\n",
		cfg.readerIndex, req.commitment, req.height, blob.DataSize(), dlLat, queueWait, e2eLat, inclusionLat)
}

func printSummary(cfg config, st *stats, elapsed time.Duration) {
	s := st.downloadsSuccess.Load()
	var avgDl, avgE2E, avgIncl, avgQueue time.Duration
	if s > 0 {
		avgDl = time.Duration(st.dlTotalLatNs.Load() / s)
		avgE2E = time.Duration(st.e2eTotalLatNs.Load() / s)
		avgIncl = time.Duration(st.inclusionLatNs.Load() / s)
		avgQueue = time.Duration(st.queueWaitNs.Load() / s)
	}

	bytes := st.downloadedBytes.Load()
	var mibPerSec float64
	if elapsed > 0 {
		mibPerSec = float64(bytes) / (1024 * 1024) / elapsed.Seconds()
	}

	fmt.Printf("\n--- Summary (reader-%d of %d) ---\n", cfg.readerIndex, cfg.readerCount)
	fmt.Printf("Duration:   %s\n", elapsed.Truncate(time.Second))
	fmt.Println()
	fmt.Println("Blobs:")
	fmt.Printf("  Seen:    %d\n", st.blobsSeen.Load())
	fmt.Printf("  Owned:   %d\n", st.blobsOwned.Load())
	fmt.Printf("  Skipped: %d\n", st.blobsSkipped.Load())
	fmt.Println()
	fmt.Println("Downloads:")
	fmt.Printf("  Successes:             %d\n", s)
	fmt.Printf("  Failures:              %d\n", st.downloadsFailed.Load())
	fmt.Printf("  Commitment mismatches: %d\n", st.commitmentMismatches.Load())
	fmt.Printf("  Bytes downloaded:      %d (%.1f MiB)\n", bytes, float64(bytes)/(1024*1024))
	fmt.Printf("  Avg throughput:        %.1f MiB/s\n", mibPerSec)
	fmt.Printf("  Avg download latency:                  %s\n", avgDl)
	fmt.Printf("  Avg queue wait (semaphore):            %s\n", avgQueue)
	fmt.Printf("  Avg e2e latency (since creation):      %s\n", avgE2E)
	fmt.Printf("  Avg inclusion->download latency:       %s\n", avgIncl)
}

func newReaderMetrics() (*readerMetrics, error) {
	m := otel.Meter("fibre-reader")
	var (
		rm  readerMetrics
		err error
	)

	rm.blobsSeen, err = m.Int64Counter("fibre_reader.blobs_seen",
		metric.WithDescription("Total MsgPayForFibre observed in blocks"),
	)
	if err != nil {
		return nil, err
	}
	rm.blobsOwned, err = m.Int64Counter("fibre_reader.blobs_owned",
		metric.WithDescription("Blobs assigned to this reader by sharding"),
	)
	if err != nil {
		return nil, err
	}
	rm.blobsSkipped, err = m.Int64Counter("fibre_reader.blobs_skipped_not_owned",
		metric.WithDescription("Blobs skipped because they belong to another reader"),
	)
	if err != nil {
		return nil, err
	}
	rm.downloadsSuccess, err = m.Int64Counter("fibre_reader.downloads_success",
		metric.WithDescription("Successful blob downloads"),
	)
	if err != nil {
		return nil, err
	}
	rm.downloadsFailed, err = m.Int64Counter("fibre_reader.downloads_failed",
		metric.WithDescription("Failed blob downloads"),
	)
	if err != nil {
		return nil, err
	}
	rm.commitmentMismatches, err = m.Int64Counter("fibre_reader.commitment_mismatches",
		metric.WithDescription("Downloads that returned ErrBlobCommitmentMismatch"),
	)
	if err != nil {
		return nil, err
	}
	rm.downloadedBytes, err = m.Int64Counter("fibre_reader.downloaded_bytes_total",
		metric.WithDescription("Total bytes successfully downloaded (rate of this gives per-reader throughput)"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, err
	}
	rm.downloadLatency, err = m.Float64Histogram("fibre_reader.download_latency_ms",
		metric.WithDescription("Time to download a blob"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}
	rm.e2eLatency, err = m.Float64Histogram("fibre_reader.e2e_latency_ms",
		metric.WithDescription("Time from promise CreationTimestamp to download success"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}
	rm.inclusionLatency, err = m.Float64Histogram("fibre_reader.inclusion_to_download_latency_ms",
		metric.WithDescription("Time from including block to download success"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}
	rm.blockProcessLatency, err = m.Float64Histogram("fibre_reader.block_processing_latency_ms",
		metric.WithDescription("Time spent processing a block (decode + scan + dispatch)"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}
	rm.queueWaitLatency, err = m.Float64Histogram("fibre_reader.queue_wait_ms",
		metric.WithDescription("Time a download goroutine spent waiting for a semaphore slot before the actual download began (saturation indicator)"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}
	return &rm, nil
}

func setupOTelMetrics(ctx context.Context, endpoint string) (func(context.Context), error) {
	exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("creating OTLP metric exporter: %w", err)
	}

	hostname, _ := os.Hostname()
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("fibre-reader"),
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

	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		_ = mp.Shutdown(ctx)
		return nil, fmt.Errorf("starting runtime metrics: %w", err)
	}

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
		semconv.ServiceName("fibre-reader"),
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
		ApplicationName:   "fibre-reader",
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

// retryStartup runs op with exponential backoff up to cfg.startupTimeout,
// retrying every connection-style failure. Validators may not be ready when
// fibre-reader starts (post-deploy gRPC restarts, transient TCP refused on
// fresh instances) — without this, fibre-reader exits immediately and the
// operator has to babysit. Bounded so a misconfigured endpoint still fails
// fast within startupTimeout instead of hanging forever.
func retryStartup(ctx context.Context, cfg config, label string, op func(context.Context) error) error {
	deadline := time.Now().Add(cfg.startupTimeout)
	backoff := time.Second
	const maxBackoff = 15 * time.Second
	attempt := 0
	for {
		attempt++
		err := op(ctx)
		if err == nil {
			if attempt > 1 {
				fmt.Fprintf(os.Stderr, "[reader-%d] %s ready after %d attempts\n",
					cfg.readerIndex, label, attempt)
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s failed after %d attempts within %s startup-timeout: %w",
				label, attempt, cfg.startupTimeout, err)
		}
		fmt.Fprintf(os.Stderr, "[reader-%d] %s attempt %d failed: %v — retrying in %s\n",
			cfg.readerIndex, label, attempt, err, backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}
