package main

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultEndpoint     = "localhost:9090"
	defaultKeyringDir   = "~/.celestia-app"
	defaultSubmitRate   = 1.0    // KB per second
	defaultBlobSize     = 1      // KB
	defaultNamespaceStr = "test" // default namespace for blobs
)

type txResult struct {
	submitTime time.Time
	commitTime time.Time
	latency    time.Duration
	txHash     string
	code       uint32
}

func main() {
	var (
		endpoint       = flag.String("grpc-endpoint", defaultEndpoint, "gRPC endpoint to connect to")
		keyringDir     = flag.String("keyring-dir", defaultKeyringDir, "Directory containing the keyring")
		submitRate     = flag.Float64("submit-rate", defaultSubmitRate, "Data submission rate (KB/sec)")
		blobSize       = flag.Int("blob-size", defaultBlobSize, "Size of blob data in KBs")
		namespaceStr   = flag.String("namespace", defaultNamespaceStr, "Namespace for blob submission")
		disableMetrics = flag.Bool("disable-metrics", false, "Disable metrics collection")
	)
	flag.Parse()

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		cancel()
	}()

	if err := monitorLatency(ctx, *endpoint, *keyringDir, *submitRate, *blobSize, *namespaceStr, *disableMetrics); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		//nolint:gocritic
		os.Exit(1)
	}
}

func monitorLatency(
	ctx context.Context,
	endpoint string,
	keyringDir string,
	submitRate float64,
	blobSize int,
	namespaceStr string,
	disableMetrics bool,
) error {
	fmt.Printf("Monitoring latency with submit rate: %f KB/s, blob size: %d KBs, namespace: %s\n", submitRate, blobSize, namespaceStr)
	fmt.Printf("Press Ctrl+C to stop\n\n")

	fmt.Println("Setting up tx client...")

	// Calculate transactions per second based on KB/s rate and blob size
	txPerSecond := submitRate / float64(blobSize)

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

	// Initialize encoding config and tx client
	txClient, err := user.SetupTxClient(ctx, kr, grpcConn, encCfg)
	if err != nil {
		return fmt.Errorf("failed to create tx client: %w", err)
	}

	fmt.Println("Submitting transactions...")

	// Setup result tracking
	var (
		results      []txResult
		resultsMux   sync.Mutex
		interval     = time.Duration(float64(time.Second) / txPerSecond)
		ticker       = time.NewTicker(interval)
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
			go func() {
				// Create random blob data
				blobData := make([]byte, blobSize*1024)
				if _, err := rand.Read(blobData); err != nil {
					fmt.Printf("Failed to generate random data: %v\n", err)
					return
				}
				blob, err := share.NewBlob(namespace, blobData, 0, nil)
				if err != nil {
					fmt.Printf("Failed to create blob: %v\n", err)
					return
				}

				submitTime := time.Now()

				// Submit transaction
				resp, err := txClient.SubmitPayForBlob(ctx, []*share.Blob{blob})
				if err != nil {
					fmt.Printf("Failed to submit tx: %v\n", err)
					return
				}

				if disableMetrics {
					return
				}

				commitTime := time.Now()

				resultsMux.Lock()
				results = append(results, txResult{
					submitTime: submitTime,
					commitTime: commitTime,
					latency:    commitTime.Sub(submitTime),
					txHash:     resp.TxHash,
					code:       resp.Code,
				})
				resultsMux.Unlock()
			}()
		}
		counter++
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
	if err := writer.Write([]string{"Submit Time", "Commit Time", "Latency (ms)", "Tx Hash", "Code"}); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Calculate statistics
	var (
		totalLatency float64
		latencies    = make([]float64, 0, len(results))
	)

	// Write results and collect statistics
	for _, result := range results {
		latencyMs := float64(result.latency.Milliseconds())
		totalLatency += latencyMs
		latencies = append(latencies, latencyMs)

		if err := writer.Write([]string{
			result.submitTime.Format(time.RFC3339Nano),
			result.commitTime.Format(time.RFC3339Nano),
			fmt.Sprintf("%.2f", latencyMs),
			result.txHash,
			fmt.Sprintf("%d", result.code),
		}); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}

	// Calculate and print statistics
	n := float64(len(results))
	if n == 0 {
		fmt.Println("No results collected")
		return nil
	}

	mean := totalLatency / n

	var variance float64
	for _, latency := range latencies {
		diff := latency - mean
		variance += diff * diff
	}
	variance /= n
	stdDev := math.Sqrt(variance)

	fmt.Printf("\nLatency Statistics:\n")
	fmt.Printf("Number of transactions: %d\n", len(results))
	fmt.Printf("Average latency: %.2f ms\n", mean)
	fmt.Printf("Standard deviation: %.2f ms\n", stdDev)
	fmt.Printf("Results written to latency_results.csv\n")

	return nil
}
