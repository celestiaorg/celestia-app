package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const StartFibreSessionName = "fibre"

func startFibreCmd() *cobra.Command {
	var (
		rootDir        string
		SSHKeyPath     string
		instances      int
		metricsAddress string
	)

	cmd := &cobra.Command{
		Use:   "start-fibre",
		Short: "Start fibre server on remote validators via SSH + tmux",
		Long:  "Starts fibre server tmux sessions on remote validators. The fibre binary must already be deployed via 'talis deploy'.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			// Select first N validators (default all)
			if instances <= 0 || instances > len(cfg.Validators) {
				instances = len(cfg.Validators)
			}
			validators := cfg.Validators[:instances]

			// Build the remote command
			// OTEL_METRICS_EXEMPLAR_FILTER=always_on attaches trace exemplars to all metric observations
			remoteCmd := "OTEL_METRICS_EXEMPLAR_FILTER=always_on fibre start --home .celestia-fibre --app-grpc-address localhost:9091"
			// Auto-enable metrics when observability nodes are configured
			if metricsAddress == "" && len(cfg.Observability) > 0 {
				metricsAddress = fmt.Sprintf("http://%s:4318", cfg.Observability[0].PublicIP)
			}
			if metricsAddress != "" {
				remoteCmd += fmt.Sprintf(" --otel-endpoint %s", metricsAddress)
			}

			fmt.Printf("Starting fibre sessions on %d validator(s)...\n", len(validators))

			if err := runScriptInTMux(validators, resolvedSSHKeyPath, remoteCmd, StartFibreSessionName, 5*time.Minute); err != nil {
				return fmt.Errorf("failed to start remote sessions: %w", err)
			}

			// Print summary
			fmt.Println()
			fmt.Println("=== fibre sessions started ===")
			fmt.Printf("  tmux session: %s\n", StartFibreSessionName)
			fmt.Printf("  log file:     /root/talis-%s.log\n", StartFibreSessionName)
			fmt.Println("  validators:")
			for _, val := range validators {
				fmt.Printf("    - %s (%s)\n", val.Name, val.PublicIP)
			}
			fmt.Println()
			fmt.Printf("  To kill all:  talis kill-session -s %s\n", StartFibreSessionName)
			fmt.Printf("  To view logs: ssh root@<ip> 'cat /root/talis-%s.log'\n", StartFibreSessionName)

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory (for config.json)")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to SSH private key (overrides env/default)")
	cmd.Flags().IntVar(&instances, "instances", 0, "number of validators to start fibre on (default all)")
	cmd.Flags().StringVar(&metricsAddress, "otel-endpoint", "", "OTLP HTTP endpoint for metrics/traces (e.g. http://host:4318; empty = disabled)")

	return cmd
}
