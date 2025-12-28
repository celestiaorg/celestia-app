package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
)

const (
	LatencyMonitorSessionName = "latency-monitor"
)

// startLatencyMonitorCmd creates a cobra command for starting the latency monitor on remote instances.
func startLatencyMonitorCmd() *cobra.Command {
	var (
		instances       int
		blobSize        int
		blobSizeMin     int
		submissionDelay string
		namespace       string
		metricsPort     int
		rootDir         string
		SSHKeyPath      string
		stop            bool
	)

	cmd := &cobra.Command{
		Use:   "latency-monitor",
		Short: "Starts or stops the latency monitor on remote validators",
		Long:  "Connects to remote validators and starts/stops the latency monitor in a detached tmux session.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			// Only operate on the number of instances that were specified
			insts := []Instance{}
			for i, val := range cfg.Validators {
				if i >= instances || i >= len(cfg.Validators) {
					break
				}
				insts = append(insts, val)
			}

			if stop {
				fmt.Printf("Stopping latency monitor on %d instance(s)...\n", len(insts))
				return stopTmuxSession(insts, resolvedSSHKeyPath, LatencyMonitorSessionName, time.Minute*5)
			}

			// Build the latency-monitor command
			latencyMonitorScript := fmt.Sprintf(
				"latency-monitor -k .celestia-app -e localhost:9091 -b %d -z %d -d %s -n %s --metrics-port %d 2>&1 | tee -a /root/latency-monitor-logs",
				blobSize,
				blobSizeMin,
				submissionDelay,
				namespace,
				metricsPort,
			)

			fmt.Println(insts, "\n", latencyMonitorScript)

			return runScriptInTMux(insts, resolvedSSHKeyPath, latencyMonitorScript, LatencyMonitorSessionName, time.Minute*5)
		},
	}

	// Define flags for the command
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to the user's SSH key (overrides environment variable and default)")
	cmd.Flags().IntVarP(&instances, "instances", "i", 1, "the number of instances of latency monitor, each ran on its own validator")
	cmd.Flags().IntVarP(&blobSize, "blob-size", "b", 1024, "the max number of bytes in each blob")
	cmd.Flags().IntVarP(&blobSizeMin, "blob-size-min", "z", 1024, "the min number of bytes in each blob")
	cmd.Flags().StringVarP(&submissionDelay, "submission-delay", "s", "4000ms", "delay between transaction submissions")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "test", "namespace for blob submission")
	cmd.Flags().IntVarP(&metricsPort, "metrics-port", "m", 9464, "port for Prometheus metrics HTTP server (0 to disable)")
	cmd.Flags().BoolVar(&stop, "stop", false, "stop the latency monitor instead of starting it")
	_ = cmd.MarkFlagRequired("instances")

	return cmd
}

const (
	gracefulShutdownPollInterval = 5 * time.Second
	gracefulShutdownTimeout      = 60 * time.Second
)

// stopTmuxSession SSHes into each remote host in parallel and gracefully stops the tmux session.
// It sends Ctrl+C to initiate graceful shutdown, polls for session termination, and falls back
// to force-killing the session if it doesn't stop within the timeout.
func stopTmuxSession(
	instances []Instance,
	sshKeyPath string,
	sessionName string,
	timeout time.Duration,
) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(instances))
	counter := atomic.Uint32{}

	for _, inst := range instances {
		wg.Add(1)
		go func(inst Instance) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			// Helper to run SSH commands
			runSSH := func(cmd string) ([]byte, error) {
				ssh := exec.CommandContext(ctx,
					"ssh",
					"-i", sshKeyPath,
					"-o", "StrictHostKeyChecking=no",
					"-o", "UserKnownHostsFile=/dev/null",
					fmt.Sprintf("root@%s", inst.PublicIP),
					cmd,
				)
				return ssh.CombinedOutput()
			}

			// Check if session exists first
			if _, err := runSSH(fmt.Sprintf("tmux has-session -t %s 2>/dev/null", sessionName)); err != nil {
				log.Printf("[%s] no %s session found, nothing to stop\n", inst.Name, sessionName)
				counter.Add(1)
				return
			}

			// Send Ctrl+C to initiate graceful shutdown
			log.Printf("[%s] sending Ctrl+C to %s session...\n", inst.Name, sessionName)
			if _, err := runSSH(fmt.Sprintf("tmux send-keys -t %s C-c", sessionName)); err != nil {
				errCh <- fmt.Errorf("[%s:%s] failed to send Ctrl+C: %v", inst.Name, inst.PublicIP, err)
				return
			}

			// Poll for session termination
			deadline := time.Now().Add(gracefulShutdownTimeout)
			for time.Now().Before(deadline) {
				time.Sleep(gracefulShutdownPollInterval)

				// Check if session still exists
				if _, err := runSSH(fmt.Sprintf("tmux has-session -t %s 2>/dev/null", sessionName)); err != nil {
					// Session no longer exists - graceful shutdown succeeded
					log.Printf("[%s] %s session gracefully stopped ✓ – %d/%d\n",
						inst.Name, sessionName, counter.Add(1), len(instances))
					return
				}

				log.Printf("[%s] %s session still running, waiting...\n", inst.Name, sessionName)
			}

			// Timeout reached - force kill the session
			log.Printf("[%s] timeout reached, force killing %s session...\n", inst.Name, sessionName)
			if out, err := runSSH(fmt.Sprintf("tmux kill-session -t %s 2>/dev/null || true", sessionName)); err != nil {
				errCh <- fmt.Errorf("[%s:%s] failed to force kill session: %v\n%s",
					inst.Name, inst.PublicIP, err, out)
				return
			}

			log.Printf("[%s] %s session force killed ⚠️ – %d/%d\n",
				sessionName, inst.Name, counter.Add(1), len(instances))
		}(inst)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) > 0 {
		sb := strings.Builder{}
		sb.WriteString("errors stopping tmux session:\n")
		for _, e := range errs {
			sb.WriteString("- ")
			sb.WriteString(e.Error())
			sb.WriteByte('\n')
		}
		return fmt.Errorf(sb.String())
	}
	return nil
}
