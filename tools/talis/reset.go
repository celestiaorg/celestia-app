package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

func resetCmd() *cobra.Command {
	var (
		rootDir    string
		cfgPath    string
		SSHKeyPath string
		validators []string
		workers    int
	)

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset the specified validators or all validators",
		Long:  "Stops the running services and removes files created by the deploy command for specified validators or all validators",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedKey := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			// Filter validators if specific ones were requested
			targetValidators := cfg.Validators
			if len(validators) > 0 {
				targetValidators = make([]Instance, 0)
				for _, v := range cfg.Validators {
					for _, requested := range validators {
						if strings.Contains(v.Name, requested) {
							targetValidators = append(targetValidators, v)
							break
						}
					}
				}
				if len(targetValidators) == 0 {
					return fmt.Errorf("no matching validators found")
				}
			}

			// Validators run the widest set of talis commands, so this removes
			// every tmux session, data dir, payload, log, trace, and binary that
			// talis subcommands drop on the host. The remote home directory is
			// /root. We deliberately leave /root/.ssh untouched so the SSH access
			// used to run this very script is preserved.
			cleanupScript := `
				for s in app txsim latency-monitor fibre fibre-txsim setup-fibre monitor fibre-reader; do
					tmux kill-session -t "$s" 2>/dev/null || true
				done
				rm -rf \
					.celestia-app .celestia-fibre .celestia-app-sync \
					logs latency-monitor-logs \
					payload payload.tar.gz reader-payload reader-payload.tar.gz \
					monitor.jsonl sync-node.log promtail.log promtail-config.yml \
					/root/talis-*.log /root/talis-*.sh \
					/tmp/talis-traces.tar.xz \
					/bin/celestia* /bin/txsim /bin/fibre /bin/fibre-txsim /bin/fibre-reader /bin/latency-monitor \
					/usr/local/bin/celestia-appd /usr/local/bin/promtail
			`
			// Run cleanup on each validator
			var wg sync.WaitGroup
			workerChan := make(chan struct{}, workers)
			for _, val := range targetValidators {
				wg.Add(1)
				go func(v Instance) {
					defer wg.Done()
					workerChan <- struct{}{}
					defer func() { <-workerChan }()
					fmt.Printf("Resetting validator %s...\n", v.Name)
					if err := runScriptInTMux([]Instance{v}, resolvedKey, cleanupScript, "cleanup", time.Minute*5); err != nil {
						fmt.Printf("Warning: error while cleaning up %s: %v\n", v.Name, err)
					}
				}(val)
			}
			wg.Wait()

			// Clean up encoder instances.
			if len(cfg.Encoders) > 0 {
				encoderCleanup := `
					for s in app fibre-txsim setup-fibre; do
						tmux kill-session -t "$s" 2>/dev/null || true
					done
					rm -rf \
						.celestia-app encoder-payload encoder-payload.tar.gz \
						/root/talis-*.log /root/talis-*.sh \
						/bin/celestia* /bin/fibre-txsim
				`
				var encWG sync.WaitGroup
				encWorkerChan := make(chan struct{}, workers)
				for _, enc := range cfg.Encoders {
					encWG.Add(1)
					go func(e Instance) {
						defer encWG.Done()
						encWorkerChan <- struct{}{}
						defer func() { <-encWorkerChan }()
						fmt.Printf("Resetting encoder %s...\n", e.Name)
						if err := runScriptInTMux([]Instance{e}, resolvedKey, encoderCleanup, "cleanup", time.Minute*5); err != nil {
							fmt.Printf("Warning: error while cleaning up %s: %v\n", e.Name, err)
						}
					}(enc)
				}
				encWG.Wait()
			}

			// Clean up reader instances (fibre-reader).
			if len(cfg.Readers) > 0 {
				readerCleanup := `
					tmux kill-session -t fibre-reader 2>/dev/null || true
					rm -rf \
						.celestia-app reader-payload reader-payload.tar.gz \
						/root/talis-*.log /root/talis-*.sh \
						/bin/fibre-reader /bin/celestia*
				`
				var rdrWG sync.WaitGroup
				rdrWorkerChan := make(chan struct{}, workers)
				for _, rdr := range cfg.Readers {
					rdrWG.Add(1)
					go func(r Instance) {
						defer rdrWG.Done()
						rdrWorkerChan <- struct{}{}
						defer func() { <-rdrWorkerChan }()
						fmt.Printf("Resetting reader %s...\n", r.Name)
						if err := runScriptInTMux([]Instance{r}, resolvedKey, readerCleanup, "cleanup", time.Minute*5); err != nil {
							fmt.Printf("Warning: error while cleaning up %s: %v\n", r.Name, err)
						}
					}(rdr)
				}
				rdrWG.Wait()
			}

			// Clean up observability stack (Grafana/Prometheus/Loki) if configured.
			if len(cfg.Observability) > 0 {
				observabilityCleanup := `
					if [ -d /root/observability/docker ]; then
						cd /root/observability/docker && docker compose down -v
					fi
					rm -rf /root/observability /root/observability-payload.tar.gz
				`
				var obsWG sync.WaitGroup
				obsWorkerChan := make(chan struct{}, workers)
				for _, obs := range cfg.Observability {
					obsWG.Add(1)
					go func(o Instance) {
						defer obsWG.Done()
						obsWorkerChan <- struct{}{}
						defer func() { <-obsWorkerChan }()
						fmt.Printf("Resetting observability node %s...\n", o.Name)
						if err := runScriptInTMux([]Instance{o}, resolvedKey, observabilityCleanup, "obs-cleanup", time.Minute*5); err != nil {
							fmt.Printf("Warning: error while cleaning up %s: %v\n", o.Name, err)
						}
					}(obs)
				}
				obsWG.Wait()
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory to load config from")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "config file name")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "override path to your SSH private key")
	cmd.Flags().StringSliceVarP(&validators, "validators", "v", []string{}, "optional list of validator names to reset (e.g. validator-0,validator-1)")
	cmd.Flags().IntVarP(&workers, "workers", "w", 10, "number of concurrent workers for parallel operations (should be > 0)")

	return cmd
}
