package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/digitalocean/godo"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

const (
	dropletSize = "c2-16vcpu-32gb"
	region      = "nyc3"
	repoURL     = "https://github.com/celestiaorg/celestia-app.git"
	goVersion   = "1.25.7"
)

func main() {
	var (
		sshKeyPath string
		iterations int
		cooldown   int
		branch     string
		noCleanup  bool
		skipBuild  bool
	)

	cmd := &cobra.Command{
		Use:   "measure-tip-sync-speed",
		Short: "Measure Celestia Mocha testnet sync-to-tip speed",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), sshKeyPath, iterations, cooldown, branch, noCleanup, skipBuild)
		},
	}

	cmd.Flags().StringVarP(&sshKeyPath, "ssh-key-path", "k", "", "SSH private key path (required)")
	cmd.Flags().IntVarP(&iterations, "iterations", "n", 1, "Number of sync iterations")
	cmd.Flags().IntVarP(&cooldown, "cooldown", "c", 30, "Cooldown seconds between iterations")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Git branch to test")
	cmd.Flags().BoolVarP(&skipBuild, "skip-build", "s", false, "Skip building celestia-appd")
	cmd.Flags().BoolVar(&noCleanup, "no-cleanup", false, "Keep droplet alive")

	if err := cmd.MarkFlagRequired("ssh-key-path"); err != nil {
		log.Fatal(err)
	}

	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

// run executes the main workflow. creates a DO droplet, runs the sync measurement, and cleans up.
func run(ctx context.Context, sshKeyPath string, iterations, cooldown int, branch string, noCleanup, skipBuild bool) error {
	token := os.Getenv("DIGITALOCEAN_TOKEN")
	if token == "" {
		return fmt.Errorf("DIGITALOCEAN_TOKEN not set")
	}

	client := godo.NewFromToken(token)

	// Read and match SSH key
	pubKey, err := os.ReadFile(sshKeyPath + ".pub")
	if err != nil {
		return fmt.Errorf("reading public key: %w", err)
	}

	doKey, err := findDOKey(ctx, client, string(pubKey))
	if err != nil {
		return err
	}

	fmt.Printf("Using SSH key: %s (DO: %s)\n\n", sshKeyPath, doKey.Name)

	// Create droplet with SSH key name
	dropletName := fmt.Sprintf("mocha-tip-sync-%s", doKey.Name)
	fmt.Printf("Creating droplet %s (%s, %s)...\n", dropletName, dropletSize, region)

	droplet, err := createDroplet(ctx, client, dropletName, doKey)
	if err != nil {
		return err
	}

	ip := getPublicIP(droplet)
	fmt.Printf("Droplet created: %s (ID: %d)\n\n", ip, droplet.ID)

	if noCleanup {
		defer fmt.Printf("\nDroplet kept: ssh root@%s\n", ip)
	} else {
		defer func() {
			fmt.Println("\nCleaning up...")
			if _, err := client.Droplets.Delete(ctx, droplet.ID); err != nil {
				fmt.Printf("Failed to delete droplet: %v\n", err)
			} else {
				fmt.Println("Droplet deleted")
			}
		}()
	}

	fmt.Println("Waiting for SSH...")
	sshClient, err := waitForSSH(ip, sshKeyPath, 5*time.Minute)
	if err != nil {
		return err
	}
	defer sshClient.Close()
	fmt.Print("SSH connected\n\n")

	if !skipBuild {
		fmt.Println("Setting up environment...")
		if err := execSSH(sshClient, setupScript(branch)); err != nil {
			return fmt.Errorf("setup failed: %w", err)
		}
		fmt.Println()
	}

	// Run measurement
	fmt.Printf("Running measurement (iterations=%d, cooldown=%ds)...\n\n", iterations, cooldown)
	measureCmd := fmt.Sprintf(`
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin:$HOME/celestia-app/build
cd $HOME/celestia-app
./scripts/mocha-measure-tip-sync.sh -n %d -c %d
`, iterations, cooldown)

	if err := execSSH(sshClient, measureCmd); err != nil {
		return fmt.Errorf("measurement failed: %w", err)
	}

	fmt.Println("\nMeasurement complete!")
	return nil
}

// findDOKey finds a Digital Ocean SSH key that matches the provided public key.
func findDOKey(ctx context.Context, client *godo.Client, pubKey string) (godo.Key, error) {
	opt := &godo.ListOptions{PerPage: 200}
	for {
		keys, resp, err := client.Keys.List(ctx, opt)
		if err != nil {
			return godo.Key{}, err
		}

		for _, key := range keys {
			if keysMatch(pubKey, key.PublicKey) {
				return key, nil
			}
		}

		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}
		page, _ := resp.Links.CurrentPage()
		opt.Page = page + 1
	}

	return godo.Key{}, fmt.Errorf("no matching SSH key in Digital Ocean. Upload your public key at https://cloud.digitalocean.com/account/security")
}

// keysMatch compares two SSH public keys by normalizing them (ignoring comments).
func keysMatch(key1, key2 string) bool {
	normalize := func(k string) string {
		parts := strings.Fields(k)
		if len(parts) >= 2 {
			return parts[0] + " " + parts[1]
		}
		return k
	}
	return normalize(strings.TrimSpace(key1)) == normalize(strings.TrimSpace(key2))
}

// createDroplet creates a new Digital Ocean droplet and waits for it to become active.
func createDroplet(ctx context.Context, client *godo.Client, name string, sshKey godo.Key) (*godo.Droplet, error) {
	req := &godo.DropletCreateRequest{
		Name:   name,
		Region: region,
		Size:   dropletSize,
		Image:  godo.DropletCreateImage{Slug: "ubuntu-22-04-x64"},
		SSHKeys: []godo.DropletCreateSSHKey{
			{ID: sshKey.ID, Fingerprint: sshKey.Fingerprint},
		},
		Tags: []string{"celestia-sync-speed", sshKey.Name},
	}

	droplet, _, err := client.Droplets.Create(ctx, req)
	if err != nil {
		return nil, err
	}

	// Wait for IP
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for droplet")
		case <-ticker.C:
			d, _, err := client.Droplets.Get(ctx, droplet.ID)
			if err != nil {
				return nil, err
			}
			if d.Status == "active" && getPublicIP(d) != "" {
				return d, nil
			}
		}
	}
}

// getPublicIP extracts the public IPv4 address from a droplet.
func getPublicIP(d *godo.Droplet) string {
	for _, net := range d.Networks.V4 {
		if net.Type == "public" {
			return net.IPAddress
		}
	}
	return ""
}

// waitForSSH polls the host until SSH is available or timeout is reached.
func waitForSSH(host, keyPath string, timeout time.Duration) (*ssh.Client, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := ssh.Dial("tcp", host+":22", config)
		if err == nil {
			if session, err := client.NewSession(); err == nil {
				session.Close()
				return client, nil
			}
			client.Close()
		}
		time.Sleep(5 * time.Second)
	}

	return nil, fmt.Errorf("SSH timeout after %v", timeout)
}

// execSSH executes a command on the SSH client and streams output to stdout/stderr.
func execSSH(client *ssh.Client, command string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if term.IsTerminal(int(os.Stdin.Fd())) {
		width, height, _ := term.GetSize(int(os.Stdin.Fd()))
		modes := ssh.TerminalModes{
			ssh.ECHO:          0,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		_ = session.RequestPty("xterm-256color", height, width, modes)
	}

	return session.Run(command)
}

// setupScript generates a bash script that installs dependencies and builds celestia-app.
func setupScript(branch string) string {
	branchCmd := ""
	if branch != "" {
		branchCmd = fmt.Sprintf("git checkout %s", branch)
	}

	return fmt.Sprintf(`#!/bin/bash
set -e

echo "Updating package lists..."
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq > /dev/null || { echo "ERROR: apt-get update failed"; exit 1; }

echo "Installing dependencies (curl, wget, git, build-essential, jq)..."
apt-get install -y -qq curl wget git build-essential jq > /dev/null || { echo "ERROR: apt-get install failed"; exit 1; }

echo "Downloading Go %s..."
cd /tmp
wget -q https://go.dev/dl/go%s.linux-amd64.tar.gz || { echo "ERROR: Go download failed"; exit 1; }

echo "Installing Go %s..."
rm -rf /usr/local/go
tar -C /usr/local -xzf go%s.linux-amd64.tar.gz || { echo "ERROR: Go extraction failed"; exit 1; }
export PATH=$PATH:/usr/local/go/bin
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin

echo "Cloning celestia-app from %s..."
cd $HOME
rm -rf celestia-app
git clone -q %s celestia-app || { echo "ERROR: git clone failed"; exit 1; }
cd celestia-app
%s

echo "Building celestia-appd (this may take a few minutes)..."
make build 2>&1 | grep -E "Error|error|FAIL|fatal" || true
if [ ! -f "./build/celestia-appd" ]; then
    echo "ERROR: build failed - celestia-appd binary not found"
    make build
    exit 1
fi

export PATH=$PATH:$(pwd)/build
echo "export PATH=\$PATH:$(pwd)/build" >> ~/.bashrc
echo "Setup complete!"
`, goVersion, goVersion, goVersion, goVersion, repoURL, repoURL, branchCmd)
}
