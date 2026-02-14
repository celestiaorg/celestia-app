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

			cleanupScript := `
				tmux kill-session -t app 2>/dev/null || true
				tmux kill-session -t txsim 2>/dev/null || true
				tmux kill-session -t latency-monitor 2>/dev/null || true
				rm -rf .celestia-app logs payload payload.tar.gz /bin/celestia* /bin/txsim
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
