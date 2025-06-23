package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
)

func upCmd() *cobra.Command {
	var rootDir string
	var cfgPath string
	var SSHPubKeyPath string
	var SSHKeyName string
	var DOAPIToken string

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Uses the config to spin up a distributed network",
		Long:  "Initialize the Talis network with the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			// overwrite the config values if flags or env vars are set
			// flag > env > config
			cfg.SSHKeyName = resolveValue(SSHKeyName, EnvVarSSHKeyName, cfg.SSHKeyName)
			cfg.SSHPubKeyPath = resolveValue(SSHPubKeyPath, EnvVarSSHKeyPath, cfg.SSHPubKeyPath)
			cfg.DigitalOceanToken = resolveValue(DOAPIToken, EnvVarDigitalOceanToken, cfg.DigitalOceanToken)

			client, err := NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if err := client.Up(cmd.Context()); err != nil {
				return fmt.Errorf("failed to spin up network: %w", err)
			}

			if err := client.cfg.Save(rootDir); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&SSHPubKeyPath, "ssh-pub-key-path", "s", "", "path to the user's SSH public key")
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")
	cmd.Flags().StringVarP(&SSHKeyName, "ssh-key-name", "n", "", "name for the SSH key")

	return cmd
}

func deployCmd() *cobra.Command {
	var rootDir string
	var cfgPath string
	var SSHKeyPath string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Uses the config to spin up a distributed network",
		Long:  "Initialize the Talis network with the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			tarPath := filepath.Join(rootDir, "payload.tar.gz")
			log.Printf("Compressing payload to %s\n", tarPath)
			tarCmd := exec.Command("tar", "--xz", "--options", "xz:compression-level=7,xz:threads=0", "-cf", tarPath, "-C", rootDir, "payload")
			if output, err := tarCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to compress payload: %w, output: %s", err, string(output))
			}
			log.Printf("✅ Payload compressed to %s\n", tarPath)

			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			log.Printf("Sending payload to validators...")
			return deployPayload(cfg.Validators, tarPath, SSHKeyPath, "/root", "payload/validator_init.sh", 7*time.Minute)
		},
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get user home directory: %v", err)
	}
	defaultKeyPath := filepath.Join(homeDir, ".ssh", "id_ed25519")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-pub-key-path", "s", defaultKeyPath, "path to the user's SSH key")

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")

	return cmd
}

// deployPayload copies a local archive to each remote host, unpacks it,
// and launches the specified remote script inside a detached tmux session.
// It runs all operations in parallel and returns an error if any host fails.
func deployPayload(
	ips []Instance,
	archivePath string, // e.g. "./payload.tar.gz"
	sshKeyPath string, // e.g. "~/.ssh/id_ed25519"
	remoteDir string, // e.g. "/root"
	remoteScript string, // e.g. "start.sh"
	timeout time.Duration, // per‐host timeout
) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(ips))
	archiveFile := path.Base(archivePath)

	counter := atomic.Uint32{}

	for _, inst := range ips {
		wg.Add(1)
		go func(inst Instance) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			scp := exec.CommandContext(ctx,
				"scp",
				"-i", sshKeyPath,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				archivePath,
				fmt.Sprintf("root@%s:%s/", inst.PublicIP, remoteDir),
			)
			if out, err := scp.CombinedOutput(); err != nil {
				errCh <- fmt.Errorf("[%s:%s] scp error in region %s: %v\n%s", inst.Name, inst.PublicIP, inst.Region, err, out)
				return
			}

			log.Printf("sent payload to instance 📦 %s: %s\n", inst.Name, inst.PublicIP)

			remoteCmd := strings.Join([]string{
				// unpack
				fmt.Sprintf("tar -xJf %s -C %s", filepath.Join(remoteDir, archiveFile), remoteDir),
				// make sure script is executable
				fmt.Sprintf("chmod +x %s", filepath.Join(remoteDir, remoteScript)),
				// start in a named, detached tmux session
				fmt.Sprintf("tmux new-session -d -s app '%s'", filepath.Join(remoteDir, remoteScript)),
			}, " && ")

			ssh := exec.CommandContext(ctx,
				"ssh",
				"-i", sshKeyPath,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				fmt.Sprintf("root@%s", inst.PublicIP),
				remoteCmd,
			)
			if out, err := ssh.CombinedOutput(); err != nil {
				errCh <- fmt.Errorf("[%s:%s] ssh error in region %s: %v\n%s", inst.Name, inst.PublicIP, inst.Region, err, out)
				return
			}
			log.Printf("started instance ✅ %s: %s (total %d/%d)\n", inst.Name, inst.PublicIP, counter.Add(1), len(ips))
		}(inst)
	}

	wg.Wait()
	close(errCh)

	var errs []error //nolint:prealloc
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) > 0 {
		sb := "deployment errors:\n"
		for _, e := range errs {
			sb += "- " + e.Error() + "\n"
		}
		return errors.New(sb)
	}
	return nil
}

func downCmd() *cobra.Command {
	var rootDir string
	var cfgPath string
	var SSHPubKeyPath string
	var SSHKeyName string
	var DOAPIToken string

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Uses the config to spin down a distributed network",
		Long:  "Destroys the Talis network with the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			// overwrite the config values if flags or env vars are set
			// flag > env > config
			cfg.SSHKeyName = resolveValue(SSHKeyName, EnvVarSSHKeyName, cfg.SSHKeyName)
			cfg.SSHPubKeyPath = resolveValue(SSHPubKeyPath, EnvVarSSHKeyPath, cfg.SSHPubKeyPath)
			cfg.DigitalOceanToken = resolveValue(DOAPIToken, EnvVarDigitalOceanToken, cfg.DigitalOceanToken)

			client, err := NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if err := client.Down(cmd.Context()); err != nil {
				return fmt.Errorf("failed to spin up network: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&SSHPubKeyPath, "ssh-pub-key-path", "s", "", "path to the user's SSH public key")
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")
	cmd.Flags().StringVarP(&SSHKeyName, "ssh-key-name", "n", "", "name for the SSH key")

	return cmd
}

// resolveValue selects a value based on priority: flag > env > config
func resolveValue(flagVal, envKey, configVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if env := os.Getenv(envKey); env != "" {
		if configVal != "" {
			log.Printf("Using %s from environment variable instead of config", envKey)
		}
		return env
	}
	return configVal
}
