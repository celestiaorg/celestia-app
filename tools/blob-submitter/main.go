package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultEndpoint     = "localhost:9091"
	defaultKeyringDir   = "~/.celestia-app"
	defaultBlobSize     = 7 * 1024 * 1024 // bytes
	maxBlobs            = 30
	defaultConcurrency  = 1            // number of concurrent blob submissions
	defaultNamespaceStr = "blobstress" // default namespace for blobs
)

type submissionResult struct {
	txHash       string
	submitTime   time.Time
	confirmed    bool
	confirmTime  *time.Time
	err          error
	releaseBlobSlot func() // Function to release blob semaphore slot
}

type stats struct {
	submitted int64
	confirmed int64
	failed    int64
}

var (
	endpoint     string
	keyringDir   string
	blobSize     int
	concurrency  int
	namespaceStr string
	accountName  string
	runStats     stats
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "blob-submitter",
		Short: "Submit blobs to celestia mempool for stress testing",
		Long:  "A tool to continuously submit blobs to the celestia network mempool for stress testing purposes",
		RunE:  runBlobSubmitter,
	}

	rootCmd.Flags().StringVar(&endpoint, "endpoint", defaultEndpoint, "gRPC endpoint to connect to")
	rootCmd.Flags().StringVar(&keyringDir, "keyring-dir", defaultKeyringDir, "Directory containing the keyring")
	rootCmd.Flags().IntVar(&blobSize, "blob-size", defaultBlobSize, "Size of blob data in bytes")
	rootCmd.Flags().IntVar(&concurrency, "concurrency", defaultConcurrency, "Number of concurrent blob submissions")
	rootCmd.Flags().StringVar(&namespaceStr, "namespace", defaultNamespaceStr, "Namespace for blob submission")
	rootCmd.Flags().StringVar(&accountName, "account", "", "Account name to use for signing (uses first available if not specified)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runBlobSubmitter(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt and termination signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		fmt.Printf("\nReceived %s signal, stopping...\n", sig)
		cancel()
	}()

	fmt.Printf("Starting blob submitter with:\n")
	fmt.Printf("  Endpoint: %s\n", endpoint)
	fmt.Printf("  Blob size: %d bytes\n", blobSize)
	fmt.Printf("  Concurrency: %d\n", concurrency)
	fmt.Printf("  Namespace: %s\n", namespaceStr)
	fmt.Printf("\nPress Ctrl+C to stop\n\n")

	// Parse namespace
	namespace, err := share.NewV0Namespace([]byte(namespaceStr))
	if err != nil {
		return fmt.Errorf("failed to parse namespace: %w", err)
	}

	// Initialize encoding config
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// Initialize keyring
	kr, err := keyring.New(app.Name, keyring.BackendTest, keyringDir, nil, encCfg.Codec)
	if err != nil {
		return fmt.Errorf("failed to initialize keyring: %w", err)
	}

	// Get account name if not specified
	if accountName == "" {
		keys, err := kr.List()
		if err != nil {
			return fmt.Errorf("failed to list keys: %w", err)
		}
		if len(keys) == 0 {
			return fmt.Errorf("no keys found in keyring")
		}
		accountName = keys[0].Name
		fmt.Printf("Using account: %s\n", accountName)
	}

	// Create gRPC connection
	grpcConn, err := grpc.NewClient(
		endpoint,
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

	// Initialize tx client
	txClient, err := user.SetupTxClient(ctx, kr, grpcConn, encCfg)
	if err != nil {
		return fmt.Errorf("failed to create tx client: %w", err)
	}

	fmt.Println("Starting blob submissions...")

	// Global semaphore to limit max in-flight blobs
	blobSemaphore := make(chan struct{}, maxBlobs)

	// Channel for submission results - limit buffer to prevent memory accumulation
	results := make(chan *submissionResult, concurrency*2)

	// WaitGroup for tracking goroutines
	var wg sync.WaitGroup

	// Start stats reporter
	wg.Add(1)
	go func() {
		defer wg.Done()
		statsReporter(ctx)
	}()

	// Start confirmation goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		confirmationWorker(ctx, txClient, results, blobSemaphore)
	}()

	// Start submission workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			submissionWorker(ctx, workerID, txClient, namespace, results, blobSemaphore)
		}(i)
	}

	// Wait for interrupt or context cancellation
	wg.Wait()

	// Close results channel and drain any remaining items to prevent goroutine leaks
	close(results)

	// Brief pause to allow confirmation worker to process remaining results
	time.Sleep(200 * time.Millisecond)

	// Print final stats
	fmt.Printf("\nFinal stats:\n")
	fmt.Printf("  Submitted: %d\n", atomic.LoadInt64(&runStats.submitted))
	fmt.Printf("  Confirmed: %d\n", atomic.LoadInt64(&runStats.confirmed))
	fmt.Printf("  Failed: %d\n", atomic.LoadInt64(&runStats.failed))

	return nil
}

func submissionWorker(ctx context.Context, workerID int, txClient *user.TxClient, namespace share.Namespace, results chan<- *submissionResult, blobSemaphore chan struct{}) {
	counter := 0
	baseDelay := 100 * time.Millisecond
	maxDelay := 5 * time.Second
	currentDelay := baseDelay

	ticker := time.NewTicker(baseDelay)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			counter++

			// Try to acquire blob semaphore slot (non-blocking)
			select {
			case blobSemaphore <- struct{}{}:
				// Successfully acquired slot, proceed with blob submission
			case <-ctx.Done():
				return
			default:
				// No available slots, skip this submission attempt
				fmt.Printf("Worker %d-%d: Max blobs (%d) reached, skipping submission\n", workerID, counter, maxBlobs)
				continue
			}

			// Create random blob data
			blobData := make([]byte, blobSize)
			if _, err := rand.Read(blobData); err != nil {
				panic(err)
			}

			blob, err := share.NewBlob(namespace, blobData, 0, nil)
			if err != nil {
				panic(err)
			}

			submitTime := time.Now()

			// Submit transaction using BroadcastPayForBlobWithAccount
			resp, err := txClient.BroadcastPayForBlobWithAccount(ctx, accountName, []*share.Blob{blob})

			result := &submissionResult{
				submitTime: submitTime,
				err:        err,
				releaseBlobSlot: func() {
					<-blobSemaphore // Release semaphore slot
				},
			}

			if err != nil {
				atomic.AddInt64(&runStats.failed, 1)
				fmt.Printf("Worker %d-%d: Failed to submit blob: %v\n", workerID, counter, err)
				// Exponential backoff on errors
				currentDelay = time.Duration(float64(currentDelay) * 1.5)
				if currentDelay > maxDelay {
					currentDelay = maxDelay
				}
			} else {
				result.txHash = resp.TxHash
				atomic.AddInt64(&runStats.submitted, 1)
				fmt.Printf("Worker %d-%d: Submitted blob, txHash: %s\n", workerID, counter, resp.TxHash)
				// Reset delay on success
				currentDelay = baseDelay
			}

			ticker.Reset(currentDelay)

			// Send result to confirmation worker (non-blocking)
			select {
			case results <- result:
			case <-ctx.Done():
				return
			default:
				// If results channel is full, skip this result to prevent blocking
				fmt.Printf("Worker %d-%d: Results channel full, dropping result\n", workerID, counter)
			}
		}
	}
}

func statsReporter(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			submitted := atomic.LoadInt64(&runStats.submitted)
			confirmed := atomic.LoadInt64(&runStats.confirmed)
			failed := atomic.LoadInt64(&runStats.failed)
			fmt.Printf("Stats: Submitted=%d, Confirmed=%d, Failed=%d\n", submitted, confirmed, failed)
		}
	}
}

func confirmationWorker(ctx context.Context, txClient *user.TxClient, results <-chan *submissionResult, blobSemaphore chan struct{}) {
	// Limit concurrent confirmations to prevent goroutine leak
	const maxConcurrentConfirmations = 50
	semaphore := make(chan struct{}, maxConcurrentConfirmations)
	var confirmWg sync.WaitGroup

	defer func() {
		// Wait for all confirmation goroutines to complete with timeout
		done := make(chan struct{})
		go func() {
			confirmWg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All goroutines completed normally
		case <-time.After(2 * time.Second):
			// Force exit after timeout
			fmt.Println("Timeout waiting for confirmation goroutines, forcing exit...")
		}
	}()

	for result := range results {
		if result.err != nil || result.txHash == "" {
			// Release blob slot immediately for failed submissions
			if result.releaseBlobSlot != nil {
				result.releaseBlobSlot()
			}
			continue
		}

		// Acquire semaphore slot
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			return
		}

		confirmWg.Add(1)
		go func(r *submissionResult) {
			defer func() {
				<-semaphore // Release confirmation semaphore slot
				confirmWg.Done()
				// Release blob semaphore slot after confirmation completes
				if r.releaseBlobSlot != nil {
					r.releaseBlobSlot()
				}
			}()

			// Create a timeout context for confirmation to prevent hanging
			confirmCtx, confirmCancel := context.WithTimeout(ctx, 5*time.Minute)
			defer confirmCancel()

			// Check if main context is already cancelled
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Use the real ConfirmTx method to check if transaction was included
			txResp, err := txClient.ConfirmTx(confirmCtx, r.txHash)

			confirmTime := time.Now()
			r.confirmTime = &confirmTime
			latency := confirmTime.Sub(r.submitTime)

			if err != nil {
				// Check if error is due to context cancellation
				if confirmCtx.Err() != nil {
					return // Silent exit on cancellation
				}
				fmt.Printf("Failed to confirm %s: %v (latency: %v)\n", r.txHash, err, latency)
				return
			}

			if txResp.Code == 0 {
				r.confirmed = true
				atomic.AddInt64(&runStats.confirmed, 1)
				fmt.Printf("Confirmed: %s (latency: %v, height: %d)\n", r.txHash, latency, txResp.Height)
			} else {
				fmt.Printf("Transaction failed: %s (code: %d, latency: %v)\n", r.txHash, txResp.Code, latency)
			}
		}(result)
	}
}
