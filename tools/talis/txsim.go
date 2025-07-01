package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	TxSimSessionName = "txsim"
)

// startTxsimCmd creates a cobra command for starting txsim on remote instances.
func startTxsimCmd() *cobra.Command {
	var (
		instances   int
		seqCount    int
		blobsPerPFB int
		startSize   int
		endSize     int
		rootDir     string
		cfgPath     string
		SSHKeyPath  string
	)

	cmd := &cobra.Command{
		Use:   "txsim",
		Short: "Starts the txsim command on remote validators",
		Long:  "Connects to remote validators and starts the txsim command in a detached tmux session.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			txsimScript := fmt.Sprintf(
				"txsim .celestia-app/ --blob %d --blob-amounts %d --blob-sizes %d-%d --grpc-endpoint localhost:9091 --feegrant > txsim.log",
				seqCount,
				blobsPerPFB,
				startSize,
				endSize,
			)

			// only spin up txsim on the number of instances that were specified.
			insts := []Instance{}
			for i, val := range cfg.Validators {
				if i >= instances || i >= len(cfg.Validators) {
					break
				}
				insts = append(insts, val)
			}

			fmt.Println(insts, "\n", txsimScript)

			return runScriptInTMux(insts, resolvedSSHKeyPath, txsimScript, TxSimSessionName, time.Minute*5)
		},
	}

	// Define flags for the command
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config") // Keep cfgPath flag for consistency with other commands, although not strictly used after LoadConfig.
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to the user's SSH key (overrides environment variable and default)")
	cmd.Flags().IntVarP(&seqCount, "sequences", "s", 1, "the number of sequences (concurrent PFB streams) ran by each txsim instance")
	cmd.Flags().IntVarP(&instances, "instances", "i", 1, "the number of instances of txsim, each ran on its own validator")
	cmd.Flags().IntVarP(&blobsPerPFB, "blobs-per-pfb", "b", 1, "the number of blobs in each PFB")
	cmd.Flags().IntVarP(&startSize, "min-blob-size", "m", 1000000, "the min number of bytes in each blob")
	cmd.Flags().IntVarP(&endSize, "max-blob-size", "x", 1900000, "the max number of bytes in each blob")
	_ = cmd.MarkFlagRequired("sequences")
	_ = cmd.MarkFlagRequired("instances")
	return cmd
}

// killTmuxSessionCmd creates a cobra command for killing a tmux session on remote validators.
func killTmuxSessionCmd() *cobra.Command {
	var (
		rootDir    string
		cfgPath    string
		SSHKeyPath string
		session    string
		timeout    time.Duration
	)

	cmd := &cobra.Command{
		Use:     "kill-session",
		Short:   "Kills a detached tmux session on remote validators",
		Long:    "Connects to remote validator nodes and kills the named tmux session (errors suppressed).",
		Aliases: []string{"k"},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			// Resolve SSH key
			resolvedKey := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			// Raw kill session (suppress errors so no output if session doesn't exist)
			killScript := fmt.Sprintf(
				"tmux kill-session -t %s 2>/dev/null",
				session,
			)

			// Run the kill script in its own tmux on each host
			return runScriptInTMux(cfg.Validators, resolvedKey, killScript, "kill", timeout)
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory to load config from")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "config file name")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to SSH private key (overrides env/default)")
	cmd.Flags().StringVarP(&session, "session", "s", "txsim", "name of the tmux session to kill")
	_ = cmd.MarkFlagRequired("session")
	cmd.Flags().DurationVarP(&timeout, "timeout", "t", time.Minute*2, "how long to wait for SSH/tmux commands to complete")

	return cmd
}
