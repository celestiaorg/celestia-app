package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const defaultMetricsPort = 26660

func stageMetricsPayload(cfg Config, rootDir, payloadDir string) error {
	metricsSrc := filepath.Join(rootDir, "metrics")
	dockerSrc := filepath.Join(metricsSrc, "docker")
	metricsDest := filepath.Join(payloadDir, "metrics")
	dockerDest := filepath.Join(metricsDest, "docker")

	if err := copyDir(dockerSrc, dockerDest); err != nil {
		return fmt.Errorf("failed to copy metrics docker assets: %w", err)
	}

	for _, script := range []string{"install_metrics.sh", "start_metrics.sh"} {
		src := filepath.Join(metricsSrc, script)
		dest := filepath.Join(metricsDest, script)
		if err := copyFile(src, dest, 0o755); err != nil {
			return fmt.Errorf("failed to copy metrics script %s: %w", script, err)
		}
	}

	groups, skipped, err := buildMetricsTargets(cfg, defaultMetricsPort, "private")
	if err != nil {
		return err
	}

	payload, err := marshalTargets(groups, true)
	if err != nil {
		return err
	}

	targetsPath := filepath.Join(dockerDest, "targets", "targets.json")
	if err := os.MkdirAll(filepath.Dir(targetsPath), 0o755); err != nil {
		return fmt.Errorf("failed to create targets directory: %w", err)
	}
	if err := os.WriteFile(targetsPath, payload, 0o644); err != nil {
		return fmt.Errorf("failed to write targets file: %w", err)
	}

	if skipped > 0 {
		log.Printf("⚠️  skipped %d nodes for metrics targets (missing private/public IP)", skipped)
	}

	return nil
}
