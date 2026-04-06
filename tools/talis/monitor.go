package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const ResourceMonitorSessionName = "monitor"

func resourceMonitorCmd() *cobra.Command {
	var (
		rootDir    string
		SSHKeyPath string
		nodes      string
		interval   int
		stop       bool
	)

	cmd := &cobra.Command{
		Use:   "resource-monitor",
		Short: "Start network and CPU monitoring on remote validators",
		Long: `Deploys a monitoring script to remote validators that records per-port
network bandwidth (iptables accounting for ports 9091, 26656, 26657) and
per-process CPU/memory usage (celestia-appd, fibre-txsim, txsim).

Output is written to /root/monitor.jsonl on each validator. Use
'talis download-resources' to collect the results.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			validators, err := filterMatchingInstances(cfg.Validators, nodes)
			if err != nil {
				return fmt.Errorf("failed to filter nodes: %w", err)
			}
			if len(validators) == 0 {
				return fmt.Errorf("no matching validators found for pattern %q", nodes)
			}

			if stop {
				fmt.Printf("Stopping monitor on %d validator(s)...\n", len(validators))
				return stopTmuxSession(validators, resolvedSSHKeyPath, ResourceMonitorSessionName, 5*time.Minute)
			}

			// Read the monitor.sh script from the scripts directory.
			scriptPath := filepath.Join(rootDir, "tools", "talis", "scripts", "monitor.sh")
			scriptBytes, err := os.ReadFile(scriptPath)
			if err != nil {
				return fmt.Errorf("failed to read monitor script %q: %w", scriptPath, err)
			}

			// Prepend the interval env var so the script picks it up.
			script := fmt.Sprintf("export MONITOR_INTERVAL=%d\n%s", interval, string(scriptBytes))

			fmt.Printf("Starting monitor on %d validator(s)...\n", len(validators))

			if err := runScriptInTMux(validators, resolvedSSHKeyPath, script, ResourceMonitorSessionName, 5*time.Minute); err != nil {
				return fmt.Errorf("failed to start monitor sessions: %w", err)
			}

			fmt.Println()
			fmt.Println("=== monitor sessions started ===")
			fmt.Printf("  tmux session: %s\n", ResourceMonitorSessionName)
			fmt.Printf("  output file:  /root/monitor.jsonl\n")
			fmt.Printf("  log file:     /root/talis-%s.log\n", ResourceMonitorSessionName)
			fmt.Println("  validators:")
			for _, val := range validators {
				fmt.Printf("    - %s (%s)\n", val.Name, val.PublicIP)
			}
			fmt.Println()
			fmt.Printf("  To stop:       talis resource-monitor --stop\n")
			fmt.Printf("  To kill:       talis kill-session -s %s\n", ResourceMonitorSessionName)
			fmt.Printf("  To download:   talis download-resources\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory (for config.json)")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to SSH private key (overrides env/default)")
	cmd.Flags().StringVarP(&nodes, "nodes", "n", "validator-*", "glob pattern for which validators to monitor")
	cmd.Flags().IntVar(&interval, "interval", 1, "sampling interval in seconds")
	cmd.Flags().BoolVar(&stop, "stop", false, "stop monitoring instead of starting it")

	return cmd
}
