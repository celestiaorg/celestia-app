package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const SpamV3SessionName = "spam-v3"

// startSpamV3Cmd creates a cobra command that starts the v3 QueuedTxClient load
// generator (spam-txclient-v3) on remote validators in a detached tmux session.
// It mirrors the txsim command but drives the async AddPayForBlob pipeline.
func startSpamV3Cmd() *cobra.Command {
	var (
		instances    int
		blobSizeKB   int
		duration     time.Duration
		queueSize    int
		rate         int
		account      string
		otelEndpoint string
		rootDir      string
		cfgPath      string
		SSHKeyPath   string
	)

	cmd := &cobra.Command{
		Use:   "spam-v3",
		Short: "Starts the v3 QueuedTxClient load generator on remote validators",
		Long:  "Connects to remote validators and starts the spam-txclient-v3 binary (async AddPayForBlob load) in a detached tmux session.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			spamScript := fmt.Sprintf(
				"spam-txclient-v3 -keyring-dir .celestia-app -account %s -endpoint localhost:9091 -blob-size-kb %d -duration %s -queue-size %d -rate %d",
				account,
				blobSizeKB,
				duration.String(),
				queueSize,
				rate,
			)

			// Auto-wire the observability OTLP endpoint when observability nodes
			// are configured, so metrics show up in Grafana (mirrors fibre-txsim).
			otel := otelEndpoint
			if otel == "" && len(cfg.Observability) > 0 {
				otel = fmt.Sprintf("http://%s:4318", cfg.Observability[0].PublicIP)
			}
			if otel != "" {
				spamScript += fmt.Sprintf(" -otel-endpoint %s", otel)
			}

			spamScript += " > spam-v3.log"

			// only spin up the load generator on the number of instances specified.
			insts := []Instance{}
			for i, val := range cfg.Validators {
				if i >= instances || i >= len(cfg.Validators) {
					break
				}
				insts = append(insts, val)
			}

			fmt.Println(insts, "\n", spamScript)

			return runScriptInTMux(insts, resolvedSSHKeyPath, spamScript, SpamV3SessionName, time.Minute*5)
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to the user's SSH key (overrides environment variable and default)")
	cmd.Flags().IntVarP(&instances, "instances", "i", 1, "the number of instances of the load generator, each ran on its own validator")
	cmd.Flags().IntVarP(&blobSizeKB, "blob-size-kb", "b", 300, "blob size in KiB")
	cmd.Flags().DurationVarP(&duration, "duration", "t", 240*time.Second, "how long each load generator runs")
	cmd.Flags().IntVarP(&queueSize, "queue-size", "q", 100, "QueuedTxClient async queue capacity")
	cmd.Flags().IntVarP(&rate, "rate", "r", 0, "attempted enqueues per second; 0 = saturate the queue")
	cmd.Flags().StringVarP(&account, "account", "a", "txsim", "keyring account that signs and pays for txs")
	cmd.Flags().StringVar(&otelEndpoint, "otel-endpoint", "", "OTLP HTTP endpoint for metrics (default: auto-detected from observability config, e.g. http://host:4318)")
	_ = cmd.MarkFlagRequired("instances")
	return cmd
}
