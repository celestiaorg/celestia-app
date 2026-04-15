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
		rootDir           string
		SSHKeyPath        string
		instances         int
		concurrency       int
		blobSize          int
		interval          time.Duration
		duration          time.Duration
		keyPrefix         string
		download          bool
		uploadOnly        bool
		pyroscopeEndpoint string
		onEncoders        bool
	)

	cmd := &cobra.Command{
		Use:   "fibre-txsim",
		Short: "Start fibre-txsim on remote validators or encoder instances via SSH + tmux",
		Long:  "Starts fibre-txsim tmux sessions on remote validators or dedicated encoder instances. The fibre-txsim binary must already be deployed via 'talis deploy' (built by 'make build-talis-bins').",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			if onEncoders {
				return startFibreTxsimOnEncoders(cfg, resolvedSSHKeyPath, instances, concurrency, blobSize, interval, duration, download, uploadOnly, pyroscopeEndpoint)
			}

			// Legacy mode: run fibre-txsim on validators themselves
			n := min(instances, len(cfg.Validators))
			validators := cfg.Validators[:n]

			// Build the remote command — binaries are copied to /bin/ by validator_init.sh
			// OTEL_METRICS_EXEMPLAR_FILTER=always_on attaches trace exemplars to all metric observations
			remoteCmd := fmt.Sprintf(
				"OTEL_METRICS_EXEMPLAR_FILTER=always_on fibre-txsim --chain-id %s --grpc-endpoint localhost:9091 --keyring-dir .celestia-app --key-prefix %s --blob-size %d --concurrency %d --interval %s --duration %s",
				cfg.ChainID,
				keyPrefix,
				blobSize,
				concurrency,
				interval,
				duration,
			)
			if download {
				remoteCmd += " --download"
			}
			if uploadOnly {
				remoteCmd += " --upload-only"
			}

			// Auto-wire observability endpoints when observability nodes are configured
			if len(cfg.Observability) > 0 {
				remoteCmd += fmt.Sprintf(" --otel-endpoint http://%s:4318", cfg.Observability[0].PublicIP)
				if pyroscopeEndpoint == "" {
					remoteCmd += fmt.Sprintf(" --pyroscope-endpoint http://%s:4040", cfg.Observability[0].PublicIP)
				}
			}
			if pyroscopeEndpoint != "" {
				remoteCmd += fmt.Sprintf(" --pyroscope-endpoint %s", pyroscopeEndpoint)
			}

			fmt.Printf("Starting fibre-txsim sessions on %d validator(s)...\n", len(validators))

			if err := runScriptInTMux(validators, resolvedSSHKeyPath, remoteCmd, FibreTxSimSessionName, 5*time.Minute); err != nil {
				return fmt.Errorf("failed to start remote sessions: %w", err)
			}

			printFibreTxsimSummary(validators)
			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory (for config.json)")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to SSH private key (overrides env/default)")
	cmd.Flags().IntVar(&instances, "instances", 1, "number of instances to start fibre-txsim on")
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "number of concurrent blob submissions per instance")
	cmd.Flags().IntVar(&blobSize, "blob-size", 1000000, "size of each blob in bytes")
	cmd.Flags().DurationVar(&interval, "interval", 0, "delay between blob submissions (0 = no delay)")
	cmd.Flags().DurationVar(&duration, "duration", 0, "how long to run (0 = until killed)")
	cmd.Flags().StringVar(&keyPrefix, "key-prefix", "fibre", "key name prefix in keyring (keys are named <prefix>-0, <prefix>-1, ...)")
	cmd.Flags().BoolVar(&download, "download", false, "enable download verification after each successful upload (downloads blob back and compares with original data)")
	cmd.Flags().BoolVar(&uploadOnly, "upload-only", false, "skip PFF transaction — only upload shards to validators without on-chain confirmation")
	cmd.Flags().StringVar(&pyroscopeEndpoint, "pyroscope-endpoint", "", "Pyroscope endpoint for continuous profiling (default: auto-detected from observability config, e.g. http://host:4040)")
	cmd.Flags().BoolVar(&onEncoders, "on-encoders", false, "run fibre-txsim on dedicated encoder instances instead of validators")

	return cmd
}

// startFibreTxsimOnEncoders launches fibre-txsim on each encoder instance.
// Each encoder is mapped to a validator (round-robin) and uses a unique key
// prefix (enc0, enc1, ...) so that their escrow accounts are independent.
func startFibreTxsimOnEncoders(cfg Config, sshKeyPath string, instances, concurrency, blobSize int, interval, duration time.Duration, download, uploadOnly bool, pyroscopeEndpoint string) error {
	if len(cfg.Encoders) == 0 {
		return fmt.Errorf("no encoder instances found in config — add encoders via 'talis add -t encoder'")
	}

	n := min(instances, len(cfg.Encoders))
	encoders := cfg.Encoders[:n]

	fmt.Printf("Starting fibre-txsim on %d encoder(s)...\n", len(encoders))

	for _, enc := range encoders {
		encIndex := extractIndexFromName(enc.Name)
		// Round-robin map encoder → validator for gRPC endpoint
		valIndex := encIndex % len(cfg.Validators)
		grpcEndpoint := fmt.Sprintf("%s:9091", cfg.Validators[valIndex].PublicIP)
		encKeyPrefix := fmt.Sprintf("enc%d", encIndex)

		remoteCmd := fmt.Sprintf(
			"OTEL_METRICS_EXEMPLAR_FILTER=always_on fibre-txsim --chain-id %s --grpc-endpoint %s --keyring-dir .celestia-app --key-prefix %s --blob-size %d --concurrency %d --interval %s --duration %s",
			cfg.ChainID,
			grpcEndpoint,
			encKeyPrefix,
			blobSize,
			concurrency,
			interval,
			duration,
		)
		if download {
			remoteCmd += " --download"
		}
		if uploadOnly {
			remoteCmd += " --upload-only"
		}

		// Auto-wire observability endpoints
		if len(cfg.Observability) > 0 {
			remoteCmd += fmt.Sprintf(" --otel-endpoint http://%s:4318", cfg.Observability[0].PublicIP)
			if pyroscopeEndpoint == "" {
				remoteCmd += fmt.Sprintf(" --pyroscope-endpoint http://%s:4040", cfg.Observability[0].PublicIP)
			}
		}
		if pyroscopeEndpoint != "" {
			remoteCmd += fmt.Sprintf(" --pyroscope-endpoint %s", pyroscopeEndpoint)
		}

		fmt.Printf("  encoder %s → validator %s (grpc=%s, keys=%s-*)\n",
			enc.Name, cfg.Validators[valIndex].Name, grpcEndpoint, encKeyPrefix)

		if err := runScriptInTMux([]Instance{enc}, sshKeyPath, remoteCmd, FibreTxSimSessionName, 5*time.Minute); err != nil {
			return fmt.Errorf("failed to start fibre-txsim on encoder %s: %w", enc.Name, err)
		}
	}

	printFibreTxsimSummary(encoders)
	return nil
}

func printFibreTxsimSummary(instances []Instance) {
	fmt.Println()
	fmt.Println("=== fibre-txsim sessions started ===")
	fmt.Printf("  tmux session: %s\n", FibreTxSimSessionName)
	fmt.Printf("  log file:     /root/talis-%s.log\n", FibreTxSimSessionName)
	fmt.Println("  instances:")
	for _, inst := range instances {
		fmt.Printf("    - %s (%s)\n", inst.Name, inst.PublicIP)
	}
	fmt.Println()
	fmt.Printf("  To kill all:  talis kill-session -s %s\n", FibreTxSimSessionName)
	fmt.Printf("  To view logs: ssh root@<ip> 'cat /root/talis-%s.log'\n", FibreTxSimSessionName)
}
