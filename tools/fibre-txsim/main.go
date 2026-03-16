package main

import (
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

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	var (
		chainID      string
		grpcEndpoint string
		keyringDir   string
		keyPrefix    string
		blobSize     int
		concurrency  int
		interval     time.Duration
		duration     time.Duration
	)

	flag.StringVar(&chainID, "chain-id", "", "chain ID of the network (unused, accepted for compatibility)")
	flag.StringVar(&grpcEndpoint, "grpc-endpoint", "localhost:9091", "gRPC endpoint")
	flag.StringVar(&keyringDir, "keyring-dir", ".celestia-app", "keyring directory")
	flag.StringVar(&keyPrefix, "key-prefix", "fibre", "key name prefix in keyring (keys are named <prefix>-0, <prefix>-1, ...)")
	flag.IntVar(&blobSize, "blob-size", 1000000, "size of each blob in bytes")
	flag.IntVar(&concurrency, "concurrency", 1, "number of concurrent blob submissions (each gets its own account)")
	flag.DurationVar(&interval, "interval", 0, "delay between blob submissions per worker (0 = no delay)")
	flag.DurationVar(&duration, "duration", 0, "how long to run (0 = until killed)")
	flag.Parse()
	_ = chainID // accepted but unused

	if err := run(grpcEndpoint, keyringDir, keyPrefix, blobSize, concurrency, interval, duration); err != nil {
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

func run(grpcEndpoint, keyringDir, keyPrefix string, blobSize, concurrency int, interval, duration time.Duration) error {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	kr, err := keyring.New(app.Name, keyring.BackendTest, keyringDir, nil, encCfg.Codec)
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
	if duration > 0 {
		ctx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
	}

	// Create a single shared fibre client
	clientCfg := fibre.DefaultClientConfig()
	clientCfg.StateAddress = grpcEndpoint
	clientCfg.DefaultKeyName = fmt.Sprintf("%s-0", keyPrefix)

	sharedFibreClient, err := fibre.NewClient(kr, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to create shared fibre client: %w", err)
	}

	if err := sharedFibreClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start shared fibre client: %w", err)
	}

	defer func() {
		if err := sharedFibreClient.Stop(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "stopping shared fibre client: %v\n", err)
		}
	}()

	// Create one worker per concurrent slot, each with its own account
	workers := make([]worker, concurrency)
	for i := range concurrency {
		keyName := fmt.Sprintf("%s-%d", keyPrefix, i)

		grpcConn, err := grpc.NewClient(
			grpcEndpoint,
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

	// Stats
	var (
		totalSent  atomic.Int64
		successes  atomic.Int64
		failures   atomic.Int64
		totalLatNs atomic.Int64
	)
	startTime := time.Now()

	fmt.Printf("\nStarting fibre blob spam with %d workers...\n", concurrency)

	// Launch each worker in its own goroutine
	var wg sync.WaitGroup
	for i, w := range workers {
		wg.Add(1)
		go func(idx int, w worker) {
			defer wg.Done()
			for ctx.Err() == nil {
				submitBlob(ctx, w, blobSize, &totalSent, &successes, &failures, &totalLatNs)
				if interval > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(interval):
					}
				}
			}
		}(i, w)
	}

	wg.Wait()

	elapsed := time.Since(startTime)
	s := successes.Load()
	f := failures.Load()
	var avgLat time.Duration
	if s > 0 {
		avgLat = time.Duration(totalLatNs.Load() / s)
	}

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Duration:   %s\n", elapsed.Truncate(time.Second))
	fmt.Printf("Total sent: %d\n", totalSent.Load())
	fmt.Printf("Successes:  %d\n", s)
	fmt.Printf("Failures:   %d\n", f)
	fmt.Printf("Avg latency (success): %s\n", avgLat)

	return nil
}

func submitBlob(ctx context.Context, w worker, blobSize int, totalSent, successes, failures *atomic.Int64, totalLatNs *atomic.Int64) {
	// Generate random namespace
	nsID := make([]byte, share.NamespaceVersionZeroIDSize)
	if _, err := rand.Read(nsID); err != nil {
		fmt.Printf("[%s] error generating namespace: %v\n", w.keyName, err)
		failures.Add(1)
		totalSent.Add(1)
		return
	}
	id := make([]byte, 0, share.NamespaceIDSize)
	id = append(id, share.NamespaceVersionZeroPrefix...)
	id = append(id, nsID...)
	ns, err := share.NewNamespace(share.NamespaceVersionZero, id)
	if err != nil {
		fmt.Printf("[%s] error creating namespace: %v\n", w.keyName, err)
		failures.Add(1)
		totalSent.Add(1)
		return
	}

	// Generate random blob data
	data := make([]byte, blobSize)
	if _, err := rand.Read(data); err != nil {
		fmt.Printf("[%s] error generating blob data: %v\n", w.keyName, err)
		failures.Add(1)
		totalSent.Add(1)
		return
	}

	t := time.Now()
	result, err := fibre.PutWithKey(ctx, w.fibreClient, w.txClient, ns, data, w.keyName)
	lat := time.Since(t)

	totalSent.Add(1)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		failures.Add(1)
		fmt.Printf("[%s] error: %v (latency=%s)\n", w.keyName, err, lat)
		return
	}

	successes.Add(1)
	totalLatNs.Add(lat.Nanoseconds())
	fmt.Printf("[%s] height=%d tx=%s latency=%s\n", w.keyName, result.Height, result.TxHash, lat)
}
