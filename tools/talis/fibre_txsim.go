package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const FibreTxSimSessionName = "fibre-txsim"

func fibreTxsimCmd() *cobra.Command {
	var (
		rootDir     string
		SSHKeyPath  string
		instances   int
		concurrency int
		blobSize    int
		interval    time.Duration
		duration    time.Duration
		keyPrefix   string
	)

	cmd := &cobra.Command{
		Use:   "fibre-txsim",
		Short: "Start fibre-txsim on remote validators via SSH + tmux",
		Long:  "Starts fibre-txsim tmux sessions on remote validators. The fibre-txsim binary must already be deployed via 'talis deploy' (built by 'make build-talis-bins').",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			// Select first N validators
			n := min(instances, len(cfg.Validators))
			validators := cfg.Validators[:n]

			// Build the remote command — fibre-txsim is already in /bin/ from the payload
			remoteCmd := fmt.Sprintf(
				"fibre-txsim --chain-id %s --grpc-endpoint localhost:9091 --keyring-dir .celestia-app --key-prefix %s --blob-size %d --concurrency %d --interval %s --duration %s",
				cfg.ChainID,
				keyPrefix,
				blobSize,
				concurrency,
				interval,
				duration,
			)

			fmt.Printf("Starting fibre-txsim sessions on %d validator(s)...\n", len(validators))

			if err := runScriptInTMux(validators, resolvedSSHKeyPath, remoteCmd, FibreTxSimSessionName, 5*time.Minute); err != nil {
				return fmt.Errorf("failed to start remote sessions: %w", err)
			}

			// Print summary
			fmt.Println()
			fmt.Println("=== fibre-txsim sessions started ===")
			fmt.Printf("  tmux session: %s\n", FibreTxSimSessionName)
			fmt.Printf("  log file:     /root/talis-%s.log\n", FibreTxSimSessionName)
			fmt.Println("  validators:")
			for _, val := range validators {
				fmt.Printf("    - %s (%s)\n", val.Name, val.PublicIP)
			}
			fmt.Println()
			fmt.Printf("  To kill all:  talis kill-session -s %s\n", FibreTxSimSessionName)
			fmt.Printf("  To view logs: ssh root@<ip> 'cat /root/talis-%s.log'\n", FibreTxSimSessionName)

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory (for config.json)")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to SSH private key (overrides env/default)")
	cmd.Flags().IntVar(&instances, "instances", 1, "number of validators to start fibre-txsim on")
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "number of concurrent blob submissions per instance")
	cmd.Flags().IntVar(&blobSize, "blob-size", 1000000, "size of each blob in bytes")
	cmd.Flags().DurationVar(&interval, "interval", 0, "delay between blob submissions (0 = no delay)")
	cmd.Flags().DurationVar(&duration, "duration", 0, "how long to run (0 = until killed)")
	cmd.Flags().StringVar(&keyPrefix, "key-prefix", "fibre", "key name prefix in keyring (keys are named <prefix>-0, <prefix>-1, ...)")

	return cmd
}
