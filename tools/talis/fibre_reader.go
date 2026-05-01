package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const FibreReaderSessionName = "fibre-reader"

func fibreReaderCmd() *cobra.Command {
	var (
		rootDir             string
		SSHKeyPath          string
		instances           int
		downloadConcurrency int
		downloadTimeout     time.Duration
		duration            time.Duration
		keyPrefix           string
		pyroscopeEndpoint   string
	)

	cmd := &cobra.Command{
		Use:   "fibre-reader",
		Short: "Start fibre-reader on remote reader instances via SSH + tmux",
		Long:  "Starts fibre-reader tmux sessions on dedicated reader instances. Each reader trails the chain via a pinned validator's RPC, scans for MsgPayForFibre, and downloads owned blobs (hash-modulo sharded across the reader cluster). The fibre-reader binary must already be deployed via 'talis deploy'.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if len(cfg.Readers) == 0 {
				return fmt.Errorf("no reader instances found in config — add readers via 'talis add -t reader'")
			}
			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			n := len(cfg.Readers)
			if instances > 0 && instances < n {
				n = instances
			}
			readers := cfg.Readers[:n]
			readerCount := len(readers)

			fmt.Printf("Starting fibre-reader on %d reader(s)...\n", readerCount)

			for _, r := range readers {
				readerIndex := extractIndexFromName(r.Name)
				valIdx := readerIndex % len(cfg.Validators)
				target := cfg.Validators[valIdx]
				rpcEndpoint := fmt.Sprintf("tcp://%s:26657", target.PrivateIP)
				grpcEndpoint := fmt.Sprintf("%s:9091", target.PrivateIP)

				remoteCmd := fmt.Sprintf(
					"OTEL_METRICS_EXEMPLAR_FILTER=always_on fibre-reader --chain-id %s --rpc-endpoint %s --grpc-endpoint %s --keyring-dir .celestia-app --key-name %s-0 --reader-index %d --reader-count %d --download-concurrency %d --download-timeout %s --duration %s",
					cfg.ChainID,
					rpcEndpoint,
					grpcEndpoint,
					keyPrefix,
					readerIndex,
					readerCount,
					downloadConcurrency,
					downloadTimeout,
					duration,
				)

				if len(cfg.Observability) > 0 {
					remoteCmd += fmt.Sprintf(" --otel-endpoint http://%s:4318", cfg.Observability[0].PublicIP)
					if pyroscopeEndpoint == "" {
						remoteCmd += fmt.Sprintf(" --pyroscope-endpoint http://%s:4040", cfg.Observability[0].PublicIP)
					}
				}
				if pyroscopeEndpoint != "" {
					remoteCmd += fmt.Sprintf(" --pyroscope-endpoint %s", pyroscopeEndpoint)
				}

				fmt.Printf("  reader %s -> validator %s (rpc=%s, grpc=%s, index=%d/%d)\n",
					r.Name, target.Name, rpcEndpoint, grpcEndpoint, readerIndex, readerCount)

				if err := runScriptInTMux([]Instance{r}, resolvedSSHKeyPath, remoteCmd, FibreReaderSessionName, 5*time.Minute); err != nil {
					return fmt.Errorf("failed to start fibre-reader on %s: %w", r.Name, err)
				}
			}

			printFibreReaderSummary(readers)
			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory (for config.json)")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to SSH private key (overrides env/default)")
	cmd.Flags().IntVar(&instances, "instances", 0, "max number of reader instances to launch (0 = all)")
	cmd.Flags().IntVar(&downloadConcurrency, "download-concurrency", 32, "max concurrent in-flight downloads per reader (semaphore bound; goroutine spawned per blob)")
	cmd.Flags().DurationVar(&downloadTimeout, "download-timeout", 2*time.Minute, "per-blob download timeout")
	cmd.Flags().DurationVar(&duration, "duration", 0, "how long to run (0 = until killed)")
	cmd.Flags().StringVar(&keyPrefix, "key-prefix", "fibre", "fibre keyring key-name prefix (only used to satisfy fibre.NewClient's key existence check; reader does not sign)")
	cmd.Flags().StringVar(&pyroscopeEndpoint, "pyroscope-endpoint", "", "Pyroscope endpoint (default: auto-detected from observability config)")

	return cmd
}

func printFibreReaderSummary(instances []Instance) {
	fmt.Println()
	fmt.Println("=== fibre-reader sessions started ===")
	fmt.Printf("  tmux session: %s\n", FibreReaderSessionName)
	fmt.Printf("  log file:     /root/talis-%s.log\n", FibreReaderSessionName)
	fmt.Println("  instances:")
	for _, inst := range instances {
		fmt.Printf("    - %s (%s)\n", inst.Name, inst.PublicIP)
	}
	fmt.Println()
	fmt.Printf("  To kill all:  talis kill-session -s %s\n", FibreReaderSessionName)
	fmt.Printf("  To view logs: ssh root@<ip> 'cat /root/talis-%s.log'\n", FibreReaderSessionName)
}
