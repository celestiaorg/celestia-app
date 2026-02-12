package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
		instances         int
		blobSize          int
		blobSizeMin       int
		submissionDelay   string
		namespace         string
		observabilityPort int
		promtailConfig    string
		rootDir           string
		SSHKeyPath        string
		stop              bool
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

			if promtailConfig == "" {
				promtailConfig = filepath.Join(rootDir, "observability", "promtail", "promtail-config.yml")
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

			// Derive Loki URL from observability public IP
			var lokiURL string
			if len(cfg.Observability) > 0 {
				if err := updateLatencyTargets(cfg, cfg.Observability[0], resolvedSSHKeyPath, insts); err != nil {
					return err
				}

				if cfg.Observability[0].PublicIP != "" {
					lokiURL = fmt.Sprintf("http://%s:3100", cfg.Observability[0].PublicIP)
					fmt.Printf("Using Loki URL from observability node: %s\n", lokiURL)
				}
			}

			latencyMonitorCmd := fmt.Sprintf(
				"stdbuf -oL latency-monitor -k .celestia-app -a txsim -e localhost:9091 -b %d -z %d -d %s -n %s --observability-port %d -w %d 2>&1 | tee -a /root/latency-monitor-logs",
				blobSize,
				blobSizeMin,
				submissionDelay,
				namespace,
				observabilityPort,
			)

			latencyMonitorScript := latencyMonitorCmd
			if lokiURL != "" {
				script, err := promtailScript(rootDir, promtailConfig, lokiURL, latencyMonitorCmd)
				if err != nil {
					return err
				}
				latencyMonitorScript = script
			}

			fmt.Printf("Starting latency monitor on %d instance(s)...\n", len(insts))

			if err := runScriptInTMux(insts, resolvedSSHKeyPath, latencyMonitorScript, LatencyMonitorSessionName, time.Minute*5); err != nil {
				return err
			}
			return verifyLatencyMonitorStart(insts, resolvedSSHKeyPath, lokiURL != "", 30*time.Second)
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
	cmd.Flags().IntVarP(&observabilityPort, "observability-port", "m", 9464, "port for Prometheus observability HTTP server (0 to disable)")
	cmd.Flags().StringVar(&promtailConfig, "promtail-config", "", "path to promtail config template (defaults to ./observability/promtail/promtail-config.yml)")
	cmd.Flags().BoolVar(&stop, "stop", false, "stop the latency monitor instead of starting it")
	_ = cmd.MarkFlagRequired("instances")

	return cmd
}

func promtailScript(rootDir, promtailConfigPath, lokiURL, latencyMonitorCmd string) (string, error) {
	configBytes, err := os.ReadFile(promtailConfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to read promtail config %q: %w", promtailConfigPath, err)
	}

	normalizedLokiURL := normalizeLokiURL(strings.TrimRight(lokiURL, "/"))
	configIncludesPushPath := strings.Contains(string(configBytes), "__LOKI_URL__/loki/api/v1/push")
	normalizedLokiURL = ensureLokiPushURL(normalizedLokiURL, configIncludesPushPath)
	renderedConfig := strings.ReplaceAll(string(configBytes), "__LOKI_URL__", normalizedLokiURL)
	configB64 := base64.StdEncoding.EncodeToString([]byte(renderedConfig))

	scriptPath := filepath.Join(rootDir, "tools", "talis", "scripts", "promtail.sh")
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read promtail script template %q: %w", scriptPath, err)
	}

	renderedScript := strings.NewReplacer(
		"__PROMTAIL_CONFIG_B64__", configB64,
		"__LATENCY_MONITOR_CMD__", latencyMonitorCmd,
	).Replace(string(scriptBytes))

	return renderedScript, nil
}

func normalizeLokiURL(raw string) string {
	if strings.HasPrefix(raw, "http:/") && !strings.HasPrefix(raw, "http://") {
		return "http://" + strings.TrimPrefix(raw, "http:/")
	}
	if strings.HasPrefix(raw, "https:/") && !strings.HasPrefix(raw, "https://") {
		return "https://" + strings.TrimPrefix(raw, "https:/")
	}
	return raw
}

func ensureLokiPushURL(lokiURL string, configIncludesPushPath bool) string {
	if configIncludesPushPath {
		return strings.TrimSuffix(lokiURL, "/loki/api/v1/push")
	}
	if strings.HasSuffix(lokiURL, "/loki/api/v1/push") {
		return lokiURL
	}
	return lokiURL + "/loki/api/v1/push"
}

// updateLatencyTargets updates the latency monitor targets on the observability monitoring node. It shows the nodes that are currently running the latency monitor.
func updateLatencyTargets(cfg Config, observabilityNode Instance, sshKeyPath string, instances []Instance) error {
	groups, skipped, err := buildObservabilityTargetsForInstances(instances, cfg, latencyMonitorMetricsPort, "public", "validator")
	if err != nil {
		return err
	}

	payload, err := marshalTargets(groups, true)
	if err != nil {
		return err
	}

	if skipped > 0 {
		log.Printf("skipped %d nodes for latency monitor targets (missing IP)", skipped)
	}

	encoded := base64.StdEncoding.EncodeToString(payload)
	remotePath := "/root/observability/docker/targets/latency_targets.json"
	writeCmd := fmt.Sprintf("printf '%%s' %q | base64 -d > %s", encoded, remotePath)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	ssh := exec.CommandContext(ctx,
		"ssh",
		"-i", sshKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", observabilityNode.PublicIP),
		writeCmd,
	)
	if out, err := ssh.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to update latency targets on %s: %w\n%s", observabilityNode.PublicIP, err, out)
	}

	log.Printf("updated latency monitor targets on observability node %s (%d entries)", observabilityNode.PublicIP, len(groups))
	return nil
}

func verifyLatencyMonitorStart(instances []Instance, sshKeyPath string, expectPromtail bool, timeout time.Duration) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(instances))

	for _, inst := range instances {
		wg.Add(1)
		go func(inst Instance) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

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

			deadline := time.Now().Add(timeout)
			for time.Now().Before(deadline) {
				if _, err := runSSH("pgrep -a latency-monitor"); err == nil {
					if !expectPromtail {
						return
					}
					if _, err := runSSH("pgrep -a promtail"); err == nil {
						return
					}
				}
				time.Sleep(2 * time.Second)
			}

			promtailOut, _ := runSSH("tail -200 /root/promtail.log 2>/dev/null || true")
			latmonOut, _ := runSSH("tail -200 /root/latency-monitor-logs 2>/dev/null || true")
			errCh <- fmt.Errorf(
				"[%s:%s] latency-monitor did not start within %s\n-- promtail.log --\n%s\n-- latency-monitor-logs --\n%s",
				inst.Name,
				inst.PublicIP,
				timeout,
				strings.TrimSpace(string(promtailOut)),
				strings.TrimSpace(string(latmonOut)),
			)
		}(inst)
	}

	wg.Wait()
	close(errCh)

	var errs []error //nolint:prealloc
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) > 0 {
		sb := strings.Builder{}
		sb.WriteString("latency-monitor failed to start on one or more hosts:\n")
		for _, e := range errs {
			sb.WriteString("- ")
			sb.WriteString(e.Error())
			sb.WriteByte('\n')
		}
		return errors.New(sb.String())
	}
	return nil
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

	errs := make([]error, 0, len(instances))
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) == 0 {
		return nil
	}

	return fmt.Errorf("errors stopping tmux session:\n%w", errors.Join(errs...))
}
