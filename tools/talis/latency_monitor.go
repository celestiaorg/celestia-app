package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	LatencyMonitorSessionName = "latency-monitor"
)

// startLatencyMonitorCmd creates a cobra command for starting the latency monitor on remote instances.
func startLatencyMonitorCmd() *cobra.Command {
	var (
		instances       int
		blobSize        int
		blobSizeMin     int
		submissionDelay string
		namespace       string
		metricsPort     int
		rootDir         string
		SSHKeyPath      string
	)

	cmd := &cobra.Command{
		Use:   "latency-monitor",
		Short: "Starts the latency monitor on remote validators",
		Long:  "Connects to remote validators and starts the latency monitor in a detached tmux session.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			// Build the latency-monitor command
			// The binary is always named "latency-monitor" (copied from either rust or go version during genesis)
			// Note: --metrics-port is only supported by the Rust version (lumina-latency-monitor)
			var latencyMonitorScript string
			if cfg.LatencyMonitorType == LatencyMonitorRust || cfg.LatencyMonitorType == "" {
				latencyMonitorScript = fmt.Sprintf(
					"latency-monitor -k .celestia-app -e localhost:9090 -b %d -z %d -d %s -n %s --metrics-port %d > latency-monitor.log 2>&1",
					blobSize,
					blobSizeMin,
					submissionDelay,
					namespace,
					metricsPort,
				)
			} else {
				latencyMonitorScript = fmt.Sprintf(
					"latency-monitor -k .celestia-app -e localhost:9090 -b %d -z %d -d %s -n %s > latency-monitor.log 2>&1",
					blobSize,
					blobSizeMin,
					submissionDelay,
					namespace,
				)
			}

			// Only spin up latency monitor on the number of instances that were specified
			insts := []Instance{}
			for i, val := range cfg.Validators {
				if i >= instances || i >= len(cfg.Validators) {
					break
				}
				insts = append(insts, val)
			}

			fmt.Println(insts, "\n", latencyMonitorScript)

			return runScriptInTMux(insts, resolvedSSHKeyPath, latencyMonitorScript, LatencyMonitorSessionName, time.Minute*5)
		},
	}

	// Define flags for the command
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to the user's SSH key (overrides environment variable and default)")
	cmd.Flags().IntVarP(&instances, "instances", "i", 1, "the number of instances of latency monitor, each ran on its own validator")
	cmd.Flags().IntVarP(&blobSize, "blob-size", "b", 1024, "the max number of bytes in each blob")
	cmd.Flags().IntVarP(&blobSizeMin, "blob-size-min", "z", 1024, "the min number of bytes in each blob")
	cmd.Flags().StringVarP(&submissionDelay, "submission-delay", "s", "4000ms", "delay between transaction submissions")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "test", "namespace for blob submission")
	cmd.Flags().IntVarP(&metricsPort, "metrics-port", "m", 9464, "port for Prometheus metrics HTTP server (0 to disable)")
	_ = cmd.MarkFlagRequired("instances")

	return cmd
}
