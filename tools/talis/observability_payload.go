package main

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
)

const (
	defaultMetricsPort        = 26660
	latencyMonitorMetricsPort = 9464
	grafanaPasswordLength     = 16
)

// generateGrafanaPassword generates a random alphanumeric password.
func generateGrafanaPassword() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	password := make([]byte, grafanaPasswordLength)
	for i := range password {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", fmt.Errorf("failed to generate random number: %w", err)
		}
		password[i] = charset[n.Int64()]
	}
	return string(password), nil
}

// stageObservabilityPayload copies the observability directory (docker-compose, Prometheus config,
// Grafana dashboards, and setup scripts) into the payload directory and generates
// the targets.json file from the config.
//
// If no observability monitoring nodes are configured, this function does nothing.
// If observability monitoring nodes are configured but observabilitySrcDir is empty, it returns an error.
func stageObservabilityPayload(cfg Config, observabilitySrcDir, payloadDir string) error {
	// Skip if no observability monitoring nodes configured
	if len(cfg.Observability) == 0 {
		return nil
	}

	// Error if observability monitoring nodes configured but no observability directory provided
	if observabilitySrcDir == "" {
		return fmt.Errorf("observability monitoring nodes are configured but --observability-dir flag not provided")
	}

	// Validate source directory exists
	if fi, err := os.Stat(observabilitySrcDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("observability directory %q does not exist or is not a directory", observabilitySrcDir)
	}

	dockerSrc := filepath.Join(observabilitySrcDir, "docker")
	observabilityDest := filepath.Join(payloadDir, "observability")
	dockerDest := filepath.Join(observabilityDest, "docker")

	if err := copyDir(dockerSrc, dockerDest); err != nil {
		return fmt.Errorf("failed to copy observability docker assets: %w", err)
	}

	for _, script := range []string{"install_metrics.sh", "start_metrics.sh"} {
		src := filepath.Join(observabilitySrcDir, script)
		dest := filepath.Join(observabilityDest, script)
		if err := copyFile(src, dest, 0o755); err != nil {
			return fmt.Errorf("failed to copy observability script %s: %w", script, err)
		}
	}

	// Generate validator observability targets (CometBFT on port 26660)
	groups, skipped, err := buildObservabilityTargets(cfg, defaultMetricsPort, "public")
	if err != nil {
		return err
	}

	payload, err := marshalTargets(groups, true)
	if err != nil {
		return err
	}

	targetsDir := filepath.Join(dockerDest, "targets")
	if err := os.MkdirAll(targetsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create targets directory: %w", err)
	}

	targetsPath := filepath.Join(targetsDir, "targets.json")
	if err := os.WriteFile(targetsPath, payload, 0o644); err != nil {
		return fmt.Errorf("failed to write targets file: %w", err)
	}

	// Generate latency monitor targets (same validators, port 9464)
	latencyGroups, _, err := buildObservabilityTargets(cfg, latencyMonitorMetricsPort, "public")
	if err != nil {
		return err
	}

	latencyPayload, err := marshalTargets(latencyGroups, true)
	if err != nil {
		return err
	}

	latencyTargetsPath := filepath.Join(targetsDir, "latency_targets.json")
	if err := os.WriteFile(latencyTargetsPath, latencyPayload, 0o644); err != nil {
		return fmt.Errorf("failed to write latency targets file: %w", err)
	}

	// Generate random Grafana password and write .env file
	grafanaPassword, err := generateGrafanaPassword()
	if err != nil {
		return fmt.Errorf("failed to generate Grafana password: %w", err)
	}
	envContent := fmt.Sprintf("GRAFANA_PASSWORD=%s\n", grafanaPassword)
	envPath := filepath.Join(dockerDest, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0o644); err != nil {
		return fmt.Errorf("failed to write .env file: %w", err)
	}

	log.Printf("staged observability payload with %d targets", len(groups))
	if skipped > 0 {
		log.Printf("⚠️  skipped %d nodes for observability targets (missing private/public IP)", skipped)
	}

	return nil
}
