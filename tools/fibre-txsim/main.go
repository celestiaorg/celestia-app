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
		keyName      string
		blobSize     int
		concurrency  int
		interval     time.Duration
		duration     time.Duration
	)

	flag.StringVar(&chainID, "chain-id", "", "chain ID of the network (unused, accepted for compatibility)")
	flag.StringVar(&grpcEndpoint, "grpc-endpoint", "localhost:9091", "gRPC endpoint")
	flag.StringVar(&keyringDir, "keyring-dir", ".celestia-app", "keyring directory")
	flag.StringVar(&keyName, "key-name", "validator", "key name in keyring")
	flag.IntVar(&blobSize, "blob-size", 1000000, "size of each blob in bytes")
	flag.IntVar(&concurrency, "concurrency", 1, "number of concurrent blob submissions")
	flag.DurationVar(&interval, "interval", 0, "delay between blob submissions (0 = no delay)")
	flag.DurationVar(&duration, "duration", 0, "how long to run (0 = until killed)")
	flag.Parse()
	_ = chainID // accepted but unused

	if err := run(grpcEndpoint, keyringDir, keyName, blobSize, concurrency, interval, duration); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(grpcEndpoint, keyringDir, keyName string, blobSize, concurrency int, interval, duration time.Duration) error {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	kr, err := keyring.New(app.Name, keyring.BackendTest, keyringDir, nil, encCfg.Codec)
	if err != nil {
		return fmt.Errorf("failed to initialize keyring: %w", err)
	}

	grpcConn, err := grpc.NewClient(
		grpcEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(math.MaxInt32),
			grpc.MaxCallRecvMsgSize(math.MaxInt32),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer grpcConn.Close()

	clientCfg := fibre.DefaultClientConfig()
	clientCfg.StateAddress = grpcEndpoint
	clientCfg.DefaultKeyName = keyName

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	txClient, err := user.SetupTxClient(ctx, kr, grpcConn, encCfg, user.WithDefaultAccount(keyName))
	if err != nil {
		return fmt.Errorf("failed to set up tx client: %w", err)
	}

	fibreClient, err := fibre.NewClient(kr, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to create fibre client: %w", err)
	}
	defer func() {
		if err := fibreClient.Stop(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "stopping fibre client: %v\n", err)
		}
	}()

	if err := fibreClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start fibre client: %w", err)
	}

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

	// Stats
	var (
		totalSent  atomic.Int64
		successes  atomic.Int64
		failures   atomic.Int64
		totalLatNs atomic.Int64
	)
	startTime := time.Now()

	// Semaphore for bounded concurrency
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	fmt.Println("\nStarting fibre blob spam...")

	// If interval is set, use a ticker to pace blob submissions.
	// Otherwise, fire as fast as the semaphore allows.
	var tick <-chan time.Time
	if interval > 0 {
		t := time.NewTicker(interval)
		defer t.Stop()
		tick = t.C
	}

	for ctx.Err() == nil {
		// Wait for the interval tick (if configured)
		if tick != nil {
			select {
			case <-ctx.Done():
				continue
			case <-tick:
			}
		}

		// Acquire semaphore slot
		select {
		case <-ctx.Done():
			continue
		case sem <- struct{}{}:
		}

		wg.Go(func() {
			defer func() { <-sem }()

			// Generate random namespace
			nsID := make([]byte, share.NamespaceVersionZeroIDSize)
			if _, err := rand.Read(nsID); err != nil {
				fmt.Printf("error generating namespace: %v\n", err)
				failures.Add(1)
				totalSent.Add(1)
				return
			}
			id := make([]byte, 0, share.NamespaceIDSize)
			id = append(id, share.NamespaceVersionZeroPrefix...)
			id = append(id, nsID...)
			ns, err := share.NewNamespace(share.NamespaceVersionZero, id)
			if err != nil {
				fmt.Printf("error creating namespace: %v\n", err)
				failures.Add(1)
				totalSent.Add(1)
				return
			}

			// Generate random blob data
			data := make([]byte, blobSize)
			if _, err := rand.Read(data); err != nil {
				fmt.Printf("error generating blob data: %v\n", err)
				failures.Add(1)
				totalSent.Add(1)
				return
			}

			t := time.Now()
			result, err := fibre.Put(ctx, fibreClient, txClient, ns, data)
			lat := time.Since(t)

			totalSent.Add(1)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				failures.Add(1)
				fmt.Printf("error: %v (latency=%s)\n", err, lat)
				return
			}

			successes.Add(1)
			totalLatNs.Add(lat.Nanoseconds())
			fmt.Printf("height=%d tx=%s latency=%s\n", result.Height, result.TxHash, lat)
		})
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
