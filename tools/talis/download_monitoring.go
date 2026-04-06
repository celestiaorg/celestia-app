package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

func downloadResourcesCmd() *cobra.Command {
	var (
		rootDir    string
		SSHKeyPath string
		nodes      string
		output     string
		workers    int
	)

	cmd := &cobra.Command{
		Use:   "download-resources",
		Short: "Download monitoring JSONL files from remote validators",
		Long: `Downloads /root/monitor.jsonl from each matching validator.
Files are saved to {output}/{validator-name}/monitor.jsonl.`,
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

			sem := make(chan struct{}, workers)
			var wg sync.WaitGroup
			var mu sync.Mutex
			downloaded := 0

			for _, val := range validators {
				wg.Add(1)
				go func(val Instance) {
					sem <- struct{}{}
					defer func() {
						wg.Done()
						<-sem
					}()

					localDir := filepath.Join(output, val.Name)
					if err := os.MkdirAll(localDir, 0o755); err != nil {
						fmt.Printf("[%s] failed to create directory %s: %v\n", val.Name, localDir, err)
						return
					}

					err := sftpDownload("/root/monitor.jsonl", localDir, "root", val.PublicIP, resolvedSSHKeyPath)
					if err != nil {
						fmt.Printf("[%s] failed to download monitor.jsonl: %v\n", val.Name, err)
						return
					}

					mu.Lock()
					downloaded++
					mu.Unlock()
					fmt.Printf("[%s] downloaded monitor.jsonl\n", val.Name)
				}(val)
			}

			wg.Wait()

			fmt.Printf("\nDownloaded monitoring data from %d/%d validator(s) to %s/\n", downloaded, len(validators), output)
			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory (for config.json)")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to SSH private key (overrides env/default)")
	cmd.Flags().StringVarP(&nodes, "nodes", "n", "validator-*", "glob pattern for which validators to download from")
	cmd.Flags().StringVarP(&output, "output", "o", "./data/monitoring/resources", "local directory to save downloaded files")
	cmd.Flags().IntVarP(&workers, "workers", "w", 10, "number of concurrent download workers")

	return cmd
}
