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
	defaultEndpoint     = "localhost:9090"
	defaultKeyringDir   = "~/.celestia-app"
	defaultBlobSize     = 7 * 1024 * 1024 // bytes
	defaultConcurrency  = 1               // number of concurrent blob submissions
	defaultNamespaceStr = "blobstress"    // default namespace for blobs
)

type submissionResult struct {
	txHash      string
	submitTime  time.Time
	confirmed   bool
	confirmTime *time.Time
	err         error
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

	// Handle interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, stopping...")
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

	// Channel for submission results
	results := make(chan *submissionResult, concurrency*10)

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
		confirmationWorker(ctx, txClient, results)
	}()

	// Start submission workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			submissionWorker(ctx, workerID, txClient, namespace, results)
		}(i)
	}

	// Wait for interrupt or context cancellation
	wg.Wait()
	close(results)

	// Print final stats
	fmt.Printf("\nFinal stats:\n")
	fmt.Printf("  Submitted: %d\n", atomic.LoadInt64(&runStats.submitted))
	fmt.Printf("  Confirmed: %d\n", atomic.LoadInt64(&runStats.confirmed))
	fmt.Printf("  Failed: %d\n", atomic.LoadInt64(&runStats.failed))

	return nil
}

func submissionWorker(ctx context.Context, workerID int, txClient *user.TxClient, namespace share.Namespace, results chan<- *submissionResult) {
	counter := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
			counter++

			// Create random blob data
			blobData := make([]byte, blobSize)
			if _, err := rand.Read(blobData); err != nil {
				atomic.AddInt64(&runStats.failed, 1)
				continue
			}

			blob, err := share.NewBlob(namespace, blobData, 0, nil)
			if err != nil {
				atomic.AddInt64(&runStats.failed, 1)
				continue
			}

			submitTime := time.Now()

			// Submit transaction using BroadcastPayForBlobWithAccount
			resp, err := txClient.BroadcastPayForBlobWithAccount(ctx, accountName, []*share.Blob{blob})

			result := &submissionResult{
				submitTime: submitTime,
				err:        err,
			}

			if err != nil {
				atomic.AddInt64(&runStats.failed, 1)
				fmt.Printf("Worker %d-%d: Failed to submit blob: %v\n", workerID, counter, err)
			} else {
				result.txHash = resp.TxHash
				atomic.AddInt64(&runStats.submitted, 1)
				fmt.Printf("Worker %d-%d: Submitted blob, txHash: %s\n", workerID, counter, resp.TxHash)
			}

			// Send result to confirmation worker
			select {
			case results <- result:
			case <-ctx.Done():
				return
			}

			// Small delay to prevent overwhelming the network
			time.Sleep(100 * time.Millisecond)
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

func confirmationWorker(ctx context.Context, txClient *user.TxClient, results <-chan *submissionResult) {
	for result := range results {
		if result.err != nil || result.txHash == "" {
			continue
		}

		// Start a goroutine for each transaction confirmation
		go func(r *submissionResult) {
			// Use the real ConfirmTx method to check if transaction was included
			txResp, err := txClient.ConfirmTx(ctx, r.txHash)

			confirmTime := time.Now()
			r.confirmTime = &confirmTime
			latency := confirmTime.Sub(r.submitTime)

			if err != nil {
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
