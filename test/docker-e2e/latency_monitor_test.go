package docker_e2e

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/stretchr/testify/require"
)

const (
	latencyMonitorImage = "ghcr.io/celestiaorg/latency-monitor"
)

// LatencyMonitorConfig configures the latency monitor process
type LatencyMonitorConfig struct {
	BlobSize    int           // Max blob size in bytes
	MinBlobSize int           // Min blob size in bytes
	Delay       time.Duration // Submission delay
}

// StressResult contains aggregated stress test metrics
type LatencyMonitorResult struct {
	TotalTxs     int
	SuccessCount int
	FailureCount int
	MaxLatency   time.Duration
	AvgLatency   time.Duration
	P99Latency   time.Duration
	SuccessRate  float64
}

// DeployLatencyMonitor starts a latency-monitor container
func (s *CelestiaTestSuite) DeployLatencyMonitor(
	ctx context.Context,
	chain tastoratypes.Chain,
	cfg LatencyMonitorConfig,
) (*tastoracontainertypes.Container, error) {
	t := s.T()

	networkName, err := getNetworkNameFromID(ctx, s.client, s.network)
	if err != nil {
		return nil, err
	}

	// Get chain tag to match latency-monitor version
	tag, err := dockerchain.GetCelestiaTagStrict()
	if err != nil {
		return nil, err
	}

	// Create job container for latency-monitor
	image := tastoracontainertypes.NewJob(
		s.logger,
		s.client,
		networkName,
		t.Name(),
		latencyMonitorImage,
		tag,
	)

	opts := tastoracontainertypes.Options{
		User: "0:0",
		// Mount chain's home directory for keyring access
		Binds: []string{chain.GetVolumeName() + ":/celestia-home"},
	}

	networkInfo, err := chain.GetNodes()[0].GetNetworkInfo(ctx)
	require.NoError(t, err, "failed to get network info")

	args := []string{
		"/bin/latency-monitor",
		"--grpc-endpoint", networkInfo.Internal.Hostname + ":9090",
		"--keyring-dir", "/celestia-home",
		// Don't specify account - will use first account from keyring alphabetically
		"--blob-size", strconv.Itoa(cfg.BlobSize),
		"--blob-size-min", strconv.Itoa(cfg.MinBlobSize),
		"--submission-delay", cfg.Delay.String(),
		"--namespace", "stresstest",
		"--disable-observability", // We'll parse CSV instead
	}

	t.Logf("Starting latency-monitor with args: %v", args)

	container, err := image.Start(ctx, args, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to start latency-monitor: %w", err)
	}

	t.Cleanup(func() {
		if err := container.Stop(10 * time.Second); err != nil {
			t.Logf("Error stopping latency-monitor: %v", err)
		}
	})

	return container, nil
}

// CollectLatencyResults sends SIGTERM to the latency-monitor container to
// trigger CSV writing, waits for it to exit, then copies and parses results.
func (s *CelestiaTestSuite) CollectLatencyResults(
	ctx context.Context,
	t *testing.T,
	containerName string,
) (*LatencyMonitorResult, error) {
	// Send SIGTERM so the latency-monitor writes the CSV and exits gracefully.
	// We use docker kill -s SIGTERM instead of container.Stop() because Stop
	// may remove the container before we can docker cp the results file.
	t.Log("Sending SIGTERM to latency-monitor...")
	killCmd := exec.CommandContext(ctx, "docker", "kill", "-s", "SIGTERM", containerName)
	if output, err := killCmd.CombinedOutput(); err != nil {
		t.Logf("Warning: docker kill failed: %v (output: %s)", err, output)
	}

	// Wait for the container to exit (up to 30s)
	t.Log("Waiting for latency-monitor to exit...")
	waitCmd := exec.CommandContext(ctx, "docker", "wait", containerName)
	if output, err := waitCmd.CombinedOutput(); err != nil {
		t.Logf("Warning: docker wait failed: %v (output: %s)", err, output)
	}

	// Copy latency_results.csv from the stopped container
	tmpFile := fmt.Sprintf("/tmp/latency_results_%d.csv", time.Now().Unix())
	srcPath := fmt.Sprintf("%s:/home/celestia/.celestia-app/latency_results.csv", containerName)

	t.Logf("Copying results from container %s to %s", containerName, tmpFile)
	cmd := exec.CommandContext(ctx, "docker", "cp", srcPath, tmpFile)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Try to get container logs for debugging
		logsCmd := exec.CommandContext(ctx, "docker", "logs", "--tail", "50", containerName)
		if logsOutput, logsErr := logsCmd.CombinedOutput(); logsErr == nil {
			t.Logf("Latency-monitor logs:\n%s", string(logsOutput))
		}
		return nil, fmt.Errorf("failed to copy results file: %w\nOutput: %s", err, output)
	}
	defer os.Remove(tmpFile)

	// Open and parse the CSV file
	file, err := os.Open(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open results file: %w", err)
	}
	defer file.Close()

	result, err := s.parseLatencyCSV(file)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV: %w", err)
	}

	t.Logf("Collected results from container: %d total txs", result.TotalTxs)
	return result, nil
}

// parseLatencyCSV parses the CSV output from latency-monitor
func (s *CelestiaTestSuite) parseLatencyCSV(r io.Reader) (*LatencyMonitorResult, error) {
	csvReader := csv.NewReader(r)

	// Read header
	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading CSV header: %w", err)
	}

	// Expected header: Submit Time, Commit Time, Latency (ms), Tx Hash, Height, Code, Failed, Error
	if len(header) < 7 {
		return nil, fmt.Errorf("invalid CSV header: %v", header)
	}

	var (
		latencies    []time.Duration
		successCount int
		failureCount int
	)

	// Read data rows
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading CSV row: %w", err)
		}

		// Parse "Failed" column (index 6)
		failed := strings.TrimSpace(record[6]) == "true"

		if failed {
			failureCount++
			continue
		}

		successCount++

		// Parse "Latency (ms)" column (index 2)
		if record[2] == "" {
			continue // Skip if no latency recorded
		}

		latencyMs, err := strconv.ParseFloat(record[2], 64)
		if err != nil {
			return nil, fmt.Errorf("parsing latency: %w", err)
		}

		latencies = append(latencies, time.Duration(latencyMs)*time.Millisecond)
	}

	totalTxs := successCount + failureCount
	if totalTxs == 0 {
		return nil, fmt.Errorf("no transactions recorded")
	}

	// Calculate statistics
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	var maxLatency, avgLatency, p99Latency time.Duration
	if len(latencies) > 0 {
		maxLatency = latencies[len(latencies)-1]

		var sum time.Duration
		for _, l := range latencies {
			sum += l
		}
		avgLatency = sum / time.Duration(len(latencies))

		p99Index := int(float64(len(latencies)) * 0.99)
		if p99Index >= len(latencies) {
			p99Index = len(latencies) - 1
		}
		p99Latency = latencies[p99Index]
	}

	return &LatencyMonitorResult{
		TotalTxs:     totalTxs,
		SuccessCount: successCount,
		FailureCount: failureCount,
		MaxLatency:   maxLatency,
		AvgLatency:   avgLatency,
		P99Latency:   p99Latency,
		SuccessRate:  float64(successCount) / float64(totalTxs),
	}, nil
}
