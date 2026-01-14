package main

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	mathrand "math/rand"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/app/encoding"
	"github.com/celestiaorg/celestia-app/v7/pkg/user"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultEndpoint        = "localhost:9090"
	defaultKeyringDir      = "~/.celestia-app"
	defaultBlobSize        = 1024                    // bytes
	defaultMinBlobSize     = 1                       // bytes
	defaultNamespaceStr    = "test"                  // default namespace for blobs
	defaultSubmissionDelay = 4000 * time.Millisecond // delay between submissions
	defaultMetricsPort     = 9464                    // default Prometheus metrics port
)

var (
	endpoint        string
	keyringDir      string
	accountName     string
	blobSize        int
	minBlobSize     int
	namespaceStr    string
	disableMetrics  bool
	submissionDelay time.Duration
	metricsPort     int
)

type txResult struct {
	submitTime time.Time
	commitTime time.Time
	latency    time.Duration
	txHash     string
	code       uint32
	height     int64
	failed     bool
	errorMsg   string
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		//nolint:gocritic
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "latency-monitor",
		Short: "Monitor and measure transaction latency in Celestia networks",
		Long: `A tool for monitoring and measuring transaction latency in Celestia networks.
This tool submits PayForBlob transactions at a specified rate and measures the time
between submission and commitment, providing detailed latency statistics.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create cancellable context
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle interrupt signal
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt)
			go func() {
				sig := <-sigChan
				fmt.Printf("\nReceived %s, shutting down gracefully...\n", sig)
				cancel()
			}()

			return monitorLatency(ctx, endpoint, keyringDir, accountName, blobSize, minBlobSize, namespaceStr, disableMetrics, submissionDelay, metricsPort)
		},
	}

	cmd.Flags().StringVarP(&endpoint, "grpc-endpoint", "e", defaultEndpoint, "gRPC endpoint to connect to")
	cmd.Flags().StringVarP(&keyringDir, "keyring-dir", "k", defaultKeyringDir, "Directory containing the keyring")
	cmd.Flags().StringVarP(&accountName, "account", "a", "", "Account name to use from keyring (defaults to first account)")
	cmd.Flags().IntVarP(&blobSize, "blob-size", "b", defaultBlobSize, "Maximum size of blob data in bytes (actual size will be random between this value and the minimum)")
	cmd.Flags().IntVarP(&minBlobSize, "blob-size-min", "z", defaultMinBlobSize, "Minimum size of blob data in bytes (actual size will be random between this value and the maximum)")
	cmd.Flags().StringVarP(&namespaceStr, "namespace", "n", defaultNamespaceStr, "Namespace for blob submission")
	cmd.Flags().BoolVarP(&disableMetrics, "disable-metrics", "m", false, "Disable metrics collection")
	cmd.Flags().DurationVarP(&submissionDelay, "submission-delay", "d", defaultSubmissionDelay, "Delay between transaction submissions")
	cmd.Flags().IntVar(&metricsPort, "metrics-port", defaultMetricsPort, "Port for Prometheus metrics HTTP server")

	return cmd
}

func monitorLatency(
	ctx context.Context,
	endpoint string,
	keyringDir string,
	accountName string,
	blobSize int,
	blobMinSize int,
	namespaceStr string,
	disableMetrics bool,
	submissionDelay time.Duration,
	metricsPort int,
) error {
	if blobMinSize < 1 {
		return fmt.Errorf("minimum blob size must be at least 1 byte (got %d)", blobMinSize)
	}
	if blobSize < blobMinSize {
		return fmt.Errorf("maximum blob size (%d) must be greater than or equal to minimum blob size (%d)", blobSize, blobMinSize)
	}

	fmt.Printf("Monitoring latency with min blob size: %d bytes, max blob size: %d bytes, submission delay: %s, namespace: %s\n",
		blobMinSize, blobSize, submissionDelay, namespaceStr)

	// Start Prometheus metrics server if metrics are enabled
	if !disableMetrics {
		startMetricsServer(metricsPort)
	}

	fmt.Printf("Press Ctrl+C to stop\n\n")

	fmt.Println("Setting up tx client...")
	fmt.Println("Note: Endpoint should be in format 'host:port' without http:// prefix (e.g., 'localhost:9090')")

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// Parse namespace from string
	namespace, err := share.NewV0Namespace([]byte(namespaceStr))
	if err != nil {
		return fmt.Errorf("failed to parse namespace: %w", err)
	}

	// Initialize keyring and get signer
	kr, err := keyring.New(app.Name, keyring.BackendTest, keyringDir, nil, encCfg.Codec)
	if err != nil {
		return fmt.Errorf("failed to initialize keyring: %w", err)
	}

	fmt.Printf("Connecting to gRPC endpoint: %s (insecure)\n", endpoint)

	grpcConn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(math.MaxInt32),
			grpc.MaxCallRecvMsgSize(math.MaxInt32),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection to %s: %w (note: this tool requires a gRPC endpoint, not REST)", endpoint, err)
	}
	defer grpcConn.Close()

	// Initialize encoding config and tx client with 1s poll time
	opts := []user.Option{user.WithPollTime(1 * time.Second)}
	if accountName != "" {
		opts = append(opts, user.WithDefaultAccount(accountName))
	}
	txClient, err := user.SetupTxClient(ctx, kr, grpcConn, encCfg, opts...)
	if err != nil {
		return fmt.Errorf("failed to create tx client: %w", err)
	}

	fmt.Printf("Using account: %s\n", txClient.DefaultAccountName())

	fmt.Println("Submitting transactions...")

	// Setup result tracking
	var (
		results      []txResult
		resultsMux   sync.Mutex
		ticker       = time.NewTicker(submissionDelay)
		updateTicker = time.NewTicker(10 * time.Second)
	)
	defer ticker.Stop()
	defer updateTicker.Stop()

	counter := 0
	// Main loop
	for {
		select {
		case <-ctx.Done():
			if disableMetrics {
				return nil
			}
			return writeResults(results)
		case <-updateTicker.C:
			fmt.Printf("Transactions submitted: %d\n", counter)
		case <-ticker.C:
			counter++
			// Create random blob data with random size (blobMinSize to blobSize bytes)
			randomSize := blobMinSize
			if blobSize > blobMinSize {
				randomSize = blobMinSize + mathrand.Intn(blobSize-blobMinSize+1)
			}
			blobData := make([]byte, randomSize)
			if _, err := rand.Read(blobData); err != nil {
				fmt.Printf("Failed to generate random data: %v\n", err)
				continue
			}
			blob, err := share.NewBlob(namespace, blobData, 0, nil)
			if err != nil {
				fmt.Printf("Failed to create blob: %v\n", err)
				continue
			}

			submitTime := time.Now()

			// Broadcast transaction without waiting for confirmation
			resp, err := txClient.BroadcastPayForBlob(ctx, []*share.Blob{blob})
			if err != nil {
				fmt.Printf("[BROADCAST_FAILED] error=%v\n", err)
				recordBroadcastFailure()
				continue
			}

			fmt.Printf("[SUBMIT] tx=%s size=%d bytes time=%s\n",
				resp.TxHash[:16], randomSize, submitTime.Format("15:04:05.000"))
			recordSubmit()

			if disableMetrics {
				continue
			}

			// Launch background goroutine to confirm the transaction
			go func(txHash string, submitTime time.Time, blobSize int) {
				confirmed, err := txClient.ConfirmTx(ctx, txHash)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						txInFlight.Dec()
						fmt.Printf("[CANCELLED] tx=%s context closed before confirmation\n", txHash[:16])
						return
					}

					recordConfirmFailure()
					resultsMux.Lock()
					// Track failed confirmation
					fmt.Printf("[FAILED] tx=%s error=%v\n", txHash[:16], err)
					results = append(results, txResult{
						submitTime: submitTime,
						commitTime: time.Now(),
						latency:    0,
						txHash:     txHash,
						code:       0,
						height:     0,
						failed:     true,
						errorMsg:   err.Error(),
					})
					resultsMux.Unlock()
					return
				}

				// Track successful confirmation
				resultsMux.Lock()
				commitTime := time.Now()
				latency := commitTime.Sub(submitTime)
				fmt.Printf("[CONFIRM] tx=%s height=%d latency=%dms code=%d time=%s\n",
					confirmed.TxHash[:16], confirmed.Height, latency.Milliseconds(), confirmed.Code, commitTime.Format("15:04:05.000"))
				recordConfirm(latency, blobSize)
				results = append(results, txResult{
					submitTime: submitTime,
					commitTime: commitTime,
					latency:    latency,
					txHash:     confirmed.TxHash,
					code:       confirmed.Code,
					height:     confirmed.Height,
					failed:     false,
					errorMsg:   "",
				})
				resultsMux.Unlock()
			}(resp.TxHash, submitTime, randomSize)
		}
	}
}

func writeResults(results []txResult) error {
	// Create CSV file
	file, err := os.Create("latency_results.csv")
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	if err := writer.Write([]string{"Submit Time", "Commit Time", "Latency (ms)", "Tx Hash", "Height", "Code", "Failed", "Error"}); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Calculate statistics
	var (
		totalLatency float64
		latencies    = make([]float64, 0, len(results))
		successCount int
		failureCount int
		totalCount   = len(results)
	)

	// Write results and collect statistics
	for _, result := range results {
		failedStr := "false"
		errorStr := ""
		if result.failed {
			failedStr = "true"
			errorStr = result.errorMsg
			failureCount++
		} else {
			latencyMs := float64(result.latency.Milliseconds())
			totalLatency += latencyMs
			latencies = append(latencies, latencyMs)
			successCount++
		}

		latencyStr := ""
		if !result.failed {
			latencyStr = fmt.Sprintf("%.2f", float64(result.latency.Milliseconds()))
		}

		if err := writer.Write([]string{
			result.submitTime.Format(time.RFC3339Nano),
			result.commitTime.Format(time.RFC3339Nano),
			latencyStr,
			result.txHash,
			fmt.Sprintf("%d", result.height),
			fmt.Sprintf("%d", result.code),
			failedStr,
			errorStr,
		}); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}

	// Calculate and print statistics
	fmt.Printf("\nTransaction Statistics:\n")
	fmt.Printf("Total transactions: %d\n", totalCount)
	fmt.Printf("Successful: %d (%.1f%%)\n", successCount, float64(successCount)/float64(totalCount)*100)
	fmt.Printf("Failed: %d (%.1f%%)\n", failureCount, float64(failureCount)/float64(totalCount)*100)

	if successCount == 0 {
		fmt.Println("No successful transactions to calculate latency statistics")
		return nil
	}

	mean := totalLatency / float64(successCount)

	var variance float64
	for _, latency := range latencies {
		diff := latency - mean
		variance += diff * diff
	}
	variance /= float64(successCount)
	stdDev := math.Sqrt(variance)

	fmt.Printf("\nLatency Statistics (successful transactions only):\n")
	fmt.Printf("Average latency: %.2f ms\n", mean)
	fmt.Printf("Standard deviation: %.2f ms\n", stdDev)
	fmt.Printf("\nResults written to latency_results.csv\n")

	return nil
}
