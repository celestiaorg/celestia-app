package docker_e2e

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"

	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
)

const latencyMonitorImage = "ghcr.io/celestiaorg/latency-monitor"

// CSV column names produced by the latency-monitor tool.
const (
	colLatencyMs = "Latency (ms)"
	colFailed    = "Failed"
)

type LatencyMonitorConfig struct {
	BlobSize        int
	MinBlobSize     int
	SubmissionDelay time.Duration
	Workers         int    // parallel worker accounts (0 or 1 = sequential)
	PrivKeyHex      string // if set, creates a keyring from hex-encoded private key
	KeyringDir      string // if set, bind-mounts this existing keyring directory
}

type LatencyMonitorResult struct {
	TotalTxs     int
	SuccessCount int
	FailureCount int
	MaxLatency   time.Duration
	AvgLatency   time.Duration
	SuccessRate  float64
}

// DeployLatencyMonitor starts a latency monitor container connected to the chain.
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

	tag, err := dockerchain.GetCelestiaTagStrict()
	if err != nil {
		return nil, err
	}

	image := tastoracontainertypes.NewJob(s.logger, s.client, networkName, t.Name(), latencyMonitorImage, tag)

	networkInfo, err := chain.GetNodes()[0].GetNetworkInfo(ctx)
	require.NoError(t, err, "failed to get network info")

	args := []string{
		"/bin/latency-monitor",
		"--grpc-endpoint", networkInfo.Internal.Hostname + ":9090",
		"--keyring-dir", "/celestia-home",
		"--blob-size", strconv.Itoa(cfg.BlobSize),
		"--blob-size-min", strconv.Itoa(cfg.MinBlobSize),
		"--submission-delay", cfg.SubmissionDelay.String(),
		"--namespace", "test",
		"--disable-observability",
	}

	t.Logf("Starting latency-monitor with args: %v", args)

	container, err := image.Start(ctx, args, tastoracontainertypes.Options{
		User:  "0:0",
		Binds: []string{chain.GetVolumeName() + ":/celestia-home"},
	})
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

// DeployLatencyMonitorForNetwork starts a latency-monitor container that connects to
// an external network (e.g., Arabica) using the given gRPC endpoint. If cfg.PrivKeyHex
// is set, a test keyring is created on disk from the private key and bind-mounted into
// the container. Unlike DeployLatencyMonitor, this does not require a local Docker chain.
func (s *CelestiaTestSuite) DeployLatencyMonitorForNetwork(
	ctx context.Context,
	grpcEndpoint string,
	cfg LatencyMonitorConfig,
) (*tastoracontainertypes.Container, error) {
	t := s.T()

	networkName, err := getNetworkNameFromID(ctx, s.client, s.network)
	if err != nil {
		return nil, err
	}

	tag := dockerchain.GetCelestiaTag()

	// Resolve keyring directory: use existing dir or create from private key.
	var keyringDir string
	switch {
	case cfg.KeyringDir != "":
		keyringDir = cfg.KeyringDir
	case cfg.PrivKeyHex != "":
		keyringDir = t.TempDir()
		if err := createKeyringFromPrivKey(keyringDir, cfg.PrivKeyHex); err != nil {
			return nil, fmt.Errorf("failed to create keyring from private key: %w", err)
		}
	default:
		return nil, fmt.Errorf("either KeyringDir or PrivKeyHex must be set")
	}

	image := tastoracontainertypes.NewJob(s.logger, s.client, networkName, t.Name(), latencyMonitorImage, tag)

	args := []string{
		"/bin/latency-monitor",
		"--grpc-endpoint", grpcEndpoint,
		"--keyring-dir", "/celestia-home",
		"--blob-size", strconv.Itoa(cfg.BlobSize),
		"--blob-size-min", strconv.Itoa(cfg.MinBlobSize),
		"--submission-delay", cfg.SubmissionDelay.String(),
		"--namespace", "loadtest",
		"--disable-observability",
	}

	if cfg.Workers > 1 {
		args = append(args, "--workers", strconv.Itoa(cfg.Workers))
	}

	t.Logf("Starting latency-monitor for external network with args: %v", args)

	container, err := image.Start(ctx, args, tastoracontainertypes.Options{
		User:  "0:0",
		Binds: []string{keyringDir + ":/celestia-home"},
	})
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

// createKeyringFromPrivKey creates a test-backend keyring at the given directory
// with a single account imported from a hex-encoded private key.
func createKeyringFromPrivKey(dir, privKeyHex string) error {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr, err := keyring.New(app.Name, keyring.BackendTest, dir, nil, encCfg.Codec)
	if err != nil {
		return fmt.Errorf("creating keyring: %w", err)
	}
	if err := kr.ImportPrivKeyHex("master", privKeyHex, "secp256k1"); err != nil {
		return fmt.Errorf("importing private key: %w", err)
	}
	return nil
}

// CollectLatencyResults sends SIGTERM to trigger CSV writing, waits for exit,
// then copies and parses the results file.
func (s *CelestiaTestSuite) CollectLatencyResults(ctx context.Context, t *testing.T, containerName string) (*LatencyMonitorResult, error) {
	// Signal the monitor to write CSV and exit
	killCmd := exec.CommandContext(ctx, "docker", "kill", "-s", "SIGTERM", containerName)
	if output, err := killCmd.CombinedOutput(); err != nil {
		t.Logf("Warning: docker kill failed: %v (output: %s)", err, output)
	}

	waitCmd := exec.CommandContext(ctx, "docker", "wait", containerName)
	if output, err := waitCmd.CombinedOutput(); err != nil {
		t.Logf("Warning: docker wait failed: %v (output: %s)", err, output)
	}

	// Copy results CSV from the stopped container
	tmpFile := filepath.Join(t.TempDir(), "latency_results.csv")
	srcPath := fmt.Sprintf("%s:/home/celestia/.celestia-app/latency_results.csv", containerName)

	cmd := exec.CommandContext(ctx, "docker", "cp", srcPath, tmpFile)
	if output, err := cmd.CombinedOutput(); err != nil {
		logsCmd := exec.CommandContext(ctx, "docker", "logs", "--tail", "50", containerName)
		if logsOutput, logsErr := logsCmd.CombinedOutput(); logsErr == nil {
			t.Logf("Latency-monitor logs:\n%s", string(logsOutput))
		}
		return nil, fmt.Errorf("failed to copy results file: %w\nOutput: %s", err, output)
	}

	file, err := os.Open(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open results file: %w", err)
	}
	defer file.Close()

	return parseLatencyCSV(file)
}

// parseLatencyCSV parses the CSV output from latency-monitor into a result.
func parseLatencyCSV(r io.Reader) (*LatencyMonitorResult, error) {
	csvReader := csv.NewReader(r)

	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading CSV header: %w", err)
	}

	colIndex := make(map[string]int, len(header))
	for i, name := range header {
		colIndex[name] = i
	}

	latencyIdx, ok := colIndex[colLatencyMs]
	if !ok {
		return nil, fmt.Errorf("missing required column %q in header: %v", colLatencyMs, header)
	}
	failedIdx, ok := colIndex[colFailed]
	if !ok {
		return nil, fmt.Errorf("missing required column %q in header: %v", colFailed, header)
	}

	var (
		totalLatency time.Duration
		maxLatency   time.Duration
		successCount int
		failureCount int
		latencyCount int
	)

	for {
		record, err := csvReader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading CSV row: %w", err)
		}

		if strings.TrimSpace(record[failedIdx]) == "true" {
			failureCount++
			continue
		}
		successCount++

		raw := record[latencyIdx]
		if raw == "" {
			continue
		}
		latencyMs, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing latency %q: %w", raw, err)
		}

		d := time.Duration(latencyMs) * time.Millisecond
		totalLatency += d
		latencyCount++
		if d > maxLatency {
			maxLatency = d
		}
	}

	totalTxs := successCount + failureCount
	if totalTxs == 0 {
		return nil, fmt.Errorf("no transactions recorded")
	}

	var avgLatency time.Duration
	if latencyCount > 0 {
		avgLatency = totalLatency / time.Duration(latencyCount)
	}

	return &LatencyMonitorResult{
		TotalTxs:     totalTxs,
		SuccessCount: successCount,
		FailureCount: failureCount,
		MaxLatency:   maxLatency,
		AvgLatency:   avgLatency,
		SuccessRate:  float64(successCount) / float64(totalTxs),
	}, nil
}
