package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// runScriptInTMux SSHes into each remote host in parallel, and launches
// the specified remoteScript inside a detached tmux session named sessionName.
// It uses the same timeout per host and returns a combined error if any fail.
func runScriptInTMux(
	instances []Instance,
	sshKeyPath string, // e.g. "~/.ssh/id_ed25519"
	remoteScript string, // e.g. "source /root/start.sh" or "celestia-appd start"
	sessionName string, // e.g. "app"
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

			// Launch in tmux and capture output to a per-session log.
			logPath := fmt.Sprintf("/root/talis-%s.log", sessionName)
			scriptPath := fmt.Sprintf("/root/talis-%s.sh", sessionName)
			encodedScript := base64.StdEncoding.EncodeToString([]byte("#!/usr/bin/env bash\n" + remoteScript + "\n"))
			fullCmd := fmt.Sprintf(
				"printf '%%s' %q | base64 -d > %s && chmod +x %s && tmux new-session -d -s %s %q",
				encodedScript,
				scriptPath,
				scriptPath,
				sessionName,
				fmt.Sprintf("bash %s > %s 2>&1", scriptPath, logPath),
			)

			ssh := exec.CommandContext(ctx,
				"ssh",
				"-i", sshKeyPath,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				fmt.Sprintf("root@%s", inst.PublicIP),
				fullCmd,
			)
			if out, err := ssh.CombinedOutput(); err != nil {
				errCh <- fmt.Errorf("[%s:%s] ssh error in %s: %v\n%s",
					inst.Name, inst.PublicIP, inst.Region, err, out)
				return
			}

			log.Printf("started %s session on %s (%s) 🏁 – %d/%d\n",
				sessionName, inst.Name, inst.PublicIP, counter.Add(1), len(instances))
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
		sb.WriteString("❌ errors running remote script:\n")
		for _, e := range errs {
			sb.WriteString("- ")
			sb.WriteString(e.Error())
			sb.WriteByte('\n')
		}
		return errors.New(sb.String())
	}
	return nil
}

// waitForTmuxSessions polls all instances until the named tmux session no longer
// exists on any of them (i.e. the script finished), or until the timeout expires.
func waitForTmuxSessions(instances []Instance, sshKeyPath, sessionName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	poll := 10 * time.Second

	remaining := make(map[string]Instance, len(instances))
	for _, inst := range instances {
		remaining[inst.Name] = inst
	}

	for len(remaining) > 0 && time.Now().Before(deadline) {
		time.Sleep(poll)

		// Check all remaining validators in parallel
		type result struct {
			name     string
			finished bool
		}
		results := make(chan result, len(remaining))
		for name, inst := range remaining {
			go func(name string, inst Instance) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				ssh := exec.CommandContext(ctx,
					"ssh",
					"-i", sshKeyPath,
					"-o", "StrictHostKeyChecking=no",
					"-o", "UserKnownHostsFile=/dev/null",
					fmt.Sprintf("root@%s", inst.PublicIP),
					fmt.Sprintf("tmux has-session -t %s 2>/dev/null", sessionName),
				)
				err := ssh.Run()
				switch {
				case err == nil:
					// tmux has-session exited 0 → session still running.
					results <- result{name: name, finished: false}
				case errors.As(err, new(*exec.ExitError)):
					// Remote command ran but returned non-zero → session gone.
					results <- result{name: name, finished: true}
				default:
					// SSH connection error (network blip, refused, etc.) →
					// cannot determine session state; treat as still running.
					log.Printf("warning: SSH probe failed for %s (%s): %v", name, inst.PublicIP, err)
					results <- result{name: name, finished: false}
				}
			}(name, inst)
		}
		for range len(remaining) {
			r := <-results
			if r.finished {
				log.Printf("%s session finished on %s (%s)\n", sessionName, r.name, remaining[r.name].PublicIP)
				delete(remaining, r.name)
			}
		}

		if len(remaining) > 0 {
			fmt.Printf("  still waiting on %d validator(s)...\n", len(remaining))
		}
	}

	if len(remaining) > 0 {
		names := make([]string, 0, len(remaining))
		for name := range remaining {
			names = append(names, name)
		}
		return fmt.Errorf("timeout waiting for %s sessions on: %s", sessionName, strings.Join(names, ", "))
	}
	return nil
}
