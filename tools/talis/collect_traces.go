package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// collectTracesCmd creates a cobra command for kicking off trace collection
func collectTracesCmd() *cobra.Command {
	var (
		rootDir    string
		cfgPath    string
		SSHKeyPath string
	)

	cmd := &cobra.Command{
		Use:     "collect-traces",
		Short:   "Collect traces from the talis network",
		Long:    "Connects to every node in the network and starts the trace-collection script in a detached tmux session.",
		Aliases: []string{"ct"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators (nodes) found in config")
			}

			resolvedKey := resolveValue(
				SSHKeyPath,
				EnvVarSSHKeyPath,
				strings.Replace(cfg.SSHPubKeyPath, ".pub", "", 1),
			)

			const sessionName = "traces"
			return runScriptInTMux(
				cfg.Validators,
				resolvedKey,
				"source /root/payload/collect-traces.sh",
				sessionName,
				time.Minute*5,
			)
		},
	}

	// define your flags
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory containing your config")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "path to your network config file")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "override path to your SSH private key")
	return cmd
}
