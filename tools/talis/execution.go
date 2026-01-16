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
