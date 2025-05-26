package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
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

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.Replace(cfg.SSHPubKeyPath, ".pub", "", -1))

			txsimScript := fmt.Sprintf(
				"txsim .celestia-app/keyring-test --blob %d --blob-amounts %d --blob-sizes %d-%d --key-path .celestia-app --grpc-endpoint localhost:9090 --feegrant",
				seqCount,
				blobsPerPFB,
				startSize,
				endSize,
			)

			insts := []Instance{}
			for i, val := range cfg.Validators {
				if i >= seqCount || i >= len(cfg.Validators) {
					break
				}
				insts = append(insts, val)
			}

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

	return cmd
}

// startTxsimOnInstances connects to each instance via SSH and starts the txsim command
// inside a detached tmux session. It runs operations in parallel.
func startTxsimOnInstances(
	ctx context.Context, // Context for cancellation and timeouts
	instances []Instance, // List of instances from config
	sshKeyPath string, // Path to the user's SSH key
	tmuxSessionName string, // Name for the remote tmux session
	txsimCommandTemplate string, // Template for the txsim command (e.g., "/path/to/txsim ... --grpc-endpoint %s:9090 ...")
) error {
	var wg sync.WaitGroup
	// Channel to collect errors from goroutines
	errCh := make(chan error, len(instances))

	// Define a per-instance timeout, similar to the bash script's TIMEOUT
	instanceTimeout := 60 * time.Second

	for _, inst := range instances {
		wg.Add(1)
		go func(inst Instance) {
			defer wg.Done()

			// Create a context with a timeout for this specific instance's SSH command
			instanceCtx, cancel := context.WithTimeout(ctx, instanceTimeout)
			defer cancel()

			// Construct the full txsim command with the instance's public IP
			txsimCommand := fmt.Sprintf(txsimCommandTemplate, inst.PublicIP)

			// The command to execute remotely via SSH: start a new detached tmux session
			// and run the txsim command within it. Use quoted arguments for robustness.
			remoteCmd := fmt.Sprintf("tmux new-session -d -s %q %q", tmuxSessionName, txsimCommand)

			// Prepare the SSH command
			ssh := exec.CommandContext(instanceCtx, // Use the context with timeout
				"ssh",
				"-i", sshKeyPath,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				fmt.Sprintf("root@%s", inst.PublicIP),
				remoteCmd, // The command to run remotely
			)

			log.Printf("Attempting to start txsim on %s (%s) with command: %q in tmux session %q...\n", inst.Name, inst.PublicIP, txsimCommand, tmuxSessionName)

			// Execute the SSH command and capture combined output (stdout and stderr)
			out, err := ssh.CombinedOutput()
			if err != nil {
				// Send the error to the error channel along with instance info and command output
				errCh <- fmt.Errorf("[%s:%s] ssh error in region %s: %v\nOutput: %s", inst.Name, inst.PublicIP, inst.Region, err, out)
				return
			}
			log.Printf("Successfully started txsim on %s (%s) in tmux session %q.\n", inst.Name, inst.PublicIP, tmuxSessionName)

			// Optionally, you could add more checks here, e.g., connect to the tmux session
			// remotely and check if the txsim process is running, but the bash script
			// doesn't do this, so we'll omit it for now.

		}(inst) // Pass the instance variable to the goroutine
	}

	// Wait for all goroutines to complete
	wg.Wait()
	// Close the error channel once all goroutines are done
	close(errCh)

	// Collect all errors from the error channel
	var errs []error
	for e := range errCh {
		errs = append(errs, e)
	}

	// If there were any errors, return a combined error
	if len(errs) > 0 {
		sb := "start-txsim errors:\n"
		for _, e := range errs {
			sb += "- " + e.Error() + "\n"
		}
		return fmt.Errorf(sb)
	}

	// Return nil if no errors occurred
	return nil
}
