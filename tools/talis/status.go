package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	var (
		rootDir string
		nodes   string
	)

	cmd := &cobra.Command{
		Use:     "status",
		Short:   "Ping a set of CometBFT nodes and report their latest block height",
		Long:    "Loads a JSON config containing validator instances, then asynchronously queries each node's /status endpoint (port 26657) and prints its latest block height.",
		Aliases: []string{"s"},
		RunE: func(cmd *cobra.Command, args []string) error { // 1) Load configuration from disk
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config from %q: %w", rootDir, err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators (nodes) found in config")
			}

			// Filter validators based on --nodes flag
			targetValidators := cfg.Validators
			if nodes != "" && nodes != "*" {
				targetValidators, err = filterMatchingInstances(cfg.Validators, nodes)
				if err != nil {
					return fmt.Errorf("failed to filter nodes: %w", err)
				}
			}

			if len(targetValidators) == 0 {
				return fmt.Errorf("no matching validators found")
			}

			var wg sync.WaitGroup
			for _, val := range targetValidators {
				ip := val.PublicIP
				if ip == "" {
					fmt.Printf("Skipping %q: no public_ip defined\n", val.Name)
					continue
				}

				wg.Add(1)
				go func(nodeName, nodeIP string) {
					defer wg.Done()

					remote := fmt.Sprintf("http://%s:26657", nodeIP)
					client, err := http.New(remote, "/websocket")
					if err != nil {
						log.Printf("Failed to create RPC client for %s (%s:26657): %v\n", nodeName, nodeIP, err)
						return
					}

					// 4) Call the typed Status endpoint
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()

					res, err := client.Status(ctx)
					if err != nil {
						log.Printf("Failed to get status from %s (%s:26657): %v\n", nodeName, nodeIP, err)
						return
					}

					height := res.SyncInfo.LatestBlockHeight

					log.Printf("%s (%s): height %d\n", nodeName, nodeIP, height)
				}(val.Name, ip)
			}

			wg.Wait()
			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory containing your config")
	cmd.Flags().StringVarP(&nodes, "nodes", "n", "*", "specify node(s) to check using pattern matching (e.g., validator-*, *-testchain-*, validator-0-*)")
	return cmd
}
