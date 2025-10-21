package latency

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	mathrand "math/rand"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type TxResult struct {
	SubmitTime time.Time
	CommitTime time.Time
	Latency    time.Duration
	TxHash     string
	Code       uint32
	Height     int64
	Failed     bool
	ErrorMsg   string
}

type Statistics struct {
	TotalTransactions int
	SuccessfulTxs     int
	FailedTxs         int
	SuccessRate       float64
	AverageLatency    time.Duration
	StandardDeviation time.Duration
}

type Config struct {
	Endpoint         string
	KeyringDir       string
	AccountName      string
	BlobSize         int
	MinBlobSize      int
	NamespaceStr     string
	SubmissionDelay  time.Duration
	TestDuration     time.Duration
	LatencyThreshold time.Duration
	DisableMetrics   bool
}

type Monitor struct {
	config     Config
	txClient   *user.TxClient
	grpcConn   *grpc.ClientConn
	namespace  share.Namespace
	results    []TxResult
	resultsMux sync.Mutex
}

// NewMonitor creates a new latency monitor instance
func NewMonitor(config Config) (*Monitor, error) {
	return &Monitor{
		config:  config,
		results: make([]TxResult, 0),
	}, nil
}

// CalculateStatistics computes statistics from the test results
func CalculateStatistics(results []TxResult, latencyThreshold time.Duration) Statistics {
	stats := Statistics{
		TotalTransactions: len(results),
	}

	if len(results) == 0 {
		return stats
	}

	var totalLatency time.Duration
	var latencies []time.Duration
	var minLatency, maxLatency time.Duration

	for _, result := range results {
		if result.Failed {
			stats.FailedTxs++
		} else {
			stats.SuccessfulTxs++
			latencies = append(latencies, result.Latency)
			totalLatency += result.Latency

			if minLatency == 0 || result.Latency < minLatency {
				minLatency = result.Latency
			}
			if result.Latency > maxLatency {
				maxLatency = result.Latency
			}
		}
	}

	stats.SuccessRate = float64(stats.SuccessfulTxs) / float64(stats.TotalTransactions) * 100

	if stats.SuccessfulTxs > 0 {
		stats.AverageLatency = totalLatency / time.Duration(stats.SuccessfulTxs)

		// Calculate standard deviation
		var variance float64
		avgMs := float64(stats.AverageLatency.Milliseconds())
		for _, latency := range latencies {
			diff := float64(latency.Milliseconds()) - avgMs
			variance += diff * diff
		}
		variance /= float64(stats.SuccessfulTxs)
		stats.StandardDeviation = time.Duration(math.Sqrt(variance)) * time.Millisecond
	}

	return stats
}

func (m *Monitor) Setup(ctx context.Context) error {
	return m.setupInternal(ctx, false, nil, share.Namespace{})
}

func (m *Monitor) SetupWithExistingClient(txClient *user.TxClient, namespace share.Namespace) error {
	return m.setupInternal(context.Background(), true, txClient, namespace)
}

// setupInternal handles both setup modes
func (m *Monitor) setupInternal(ctx context.Context, useExistingClient bool, existingTxClient *user.TxClient, existingNamespace share.Namespace) error {
	if useExistingClient {
		m.txClient = existingTxClient
		m.namespace = existingNamespace
		return nil
	}

	// Use the MonitorLatency function for setup logic
	if m.config.MinBlobSize < 1 {
		return fmt.Errorf("minimum blob size must be at least 1 byte (got %d)", m.config.MinBlobSize)
	}

	if m.config.BlobSize < m.config.MinBlobSize {
		return fmt.Errorf("maximum blob size (%d) must be greater than or equal to minimum blob size (%d)", m.config.BlobSize, m.config.MinBlobSize)
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// Parse namespace from string
	namespace, err := share.NewV0Namespace([]byte(m.config.NamespaceStr))
	if err != nil {
		return fmt.Errorf("failed to parse namespace: %w", err)
	}
	m.namespace = namespace

	// Initialize keyring
	kr, err := keyring.New(app.Name, keyring.BackendTest, m.config.KeyringDir, nil, encCfg.Codec)
	if err != nil {
		return fmt.Errorf("failed to initialize keyring: %w", err)
	}

	// Create gRPC connection
	grpcConn, err := grpc.NewClient(
		m.config.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(math.MaxInt32),
			grpc.MaxCallRecvMsgSize(math.MaxInt32),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection to %s: %w", m.config.Endpoint, err)
	}
	m.grpcConn = grpcConn

	// Initialize tx client
	opts := []user.Option{user.WithPollTime(1 * time.Second)}
	if m.config.AccountName != "" {
		opts = append(opts, user.WithDefaultAccount(m.config.AccountName))
	}
	txClient, err := user.SetupTxClient(ctx, kr, grpcConn, encCfg, opts...)
	if err != nil {
		return fmt.Errorf("failed to create tx client: %w", err)
	}
	m.txClient = txClient

	return nil
}

// Run executes the latency loop using the provided client.
// TODO: specify that should be called after initialisation of the monitor
func (m *Monitor) Run(ctx context.Context) ([]TxResult, error) {
	if m.txClient == nil {
		return nil, errors.New("txClient not initialized; call SetupWithExistingClient first")
	}
	if m.config.MinBlobSize < 1 {
		return nil, fmt.Errorf("min blob size must be at least 1 byte")
	}
	if m.config.BlobSize < m.config.MinBlobSize {
		return nil, fmt.Errorf("max blob size (%d) must be >= min blob size (%d)", m.config.BlobSize, m.config.MinBlobSize)
	}

	fmt.Printf("Starting latency monitor (min=%dB, max=%dB, delay=%s)\n",
		m.config.MinBlobSize, m.config.BlobSize, m.config.SubmissionDelay)

	var (
		results    []TxResult
		resultsMux sync.Mutex
		ticker     = time.NewTicker(m.config.SubmissionDelay)
	)
	defer ticker.Stop()

	counter := 0
	for {
		select {
		case <-ctx.Done():
			return results, nil

		case <-ticker.C:
			counter++
			randomSize := m.config.MinBlobSize
			if m.config.BlobSize > m.config.MinBlobSize {
				randomSize += mathrand.Intn(m.config.BlobSize - m.config.MinBlobSize + 1)
			}
			data := make([]byte, randomSize)
			if _, err := rand.Read(data); err != nil {
				fmt.Printf("failed to generate random data: %v\n", err)
				continue
			}

			blob, err := share.NewBlob(m.namespace, data, 0, nil)
			if err != nil {
				fmt.Printf("failed to create blob: %v\n", err)
				continue
			}

			submitTime := time.Now()
			resp, err := m.txClient.BroadcastPayForBlob(ctx, []*share.Blob{blob})
			if err != nil {
				fmt.Printf("[BROADCAST FAIL] %v\n", err)
				continue
			}

			fmt.Printf("[SUBMIT] tx=%s size=%dB time=%s\n",
				resp.TxHash[:16], randomSize, submitTime.Format("15:04:05.000"))

			if m.config.DisableMetrics {
				continue
			}

			// confirmation loop
			go func(txHash string, submitTime time.Time) {
				confirmed, err := m.txClient.ConfirmTx(ctx, txHash)
				resultsMux.Lock()
				defer resultsMux.Unlock()

				if err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					fmt.Printf("[FAILED] tx=%s error=%v\n", txHash[:16], err)
					results = append(results, TxResult{
						SubmitTime: submitTime,
						CommitTime: time.Now(),
						Failed:     true,
						ErrorMsg:   err.Error(),
						TxHash:     txHash,
					})
					return
				}

				commitTime := time.Now()
				results = append(results, TxResult{
					SubmitTime: submitTime,
					CommitTime: commitTime,
					Latency:    commitTime.Sub(submitTime),
					TxHash:     confirmed.TxHash,
					Code:       confirmed.Code,
					Height:     confirmed.Height,
				})
				fmt.Printf("[CONFIRM] tx=%s height=%d latency=%dms\n",
					confirmed.TxHash[:16], confirmed.Height, time.Since(submitTime).Milliseconds())
			}(resp.TxHash, submitTime)
		}
	}
}

// GetResults returns a copy of the current results
func (m *Monitor) GetResults() []TxResult {
	m.resultsMux.Lock()
	defer m.resultsMux.Unlock()

	results := make([]TxResult, len(m.results))
	copy(results, m.results)
	return results
}

// CalculateStatistics computes statistics from the monitor's results
func (m *Monitor) CalculateStatistics() Statistics {
	results := m.GetResults()
	return CalculateStatistics(results, m.config.LatencyThreshold)
}

// Cleanup closes connections and performs cleanup
func (m *Monitor) Cleanup() {
	if m.grpcConn != nil {
		m.grpcConn.Close()
	}
}
