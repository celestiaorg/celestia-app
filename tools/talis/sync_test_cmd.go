package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const (
	syncTestMachineType = "c3d-highcpu-16"
	syncTestDiskSizeGB  = 400
)

func syncTestCmd() *cobra.Command {
	var (
		rootDir       string
		sshPubKeyPath string
		sshKeyPath    string
		gcProject     string
		gcKeyJSONPath string
		region        string
		iterations    int
		cooldown      int
		keep          bool
		binaryPath    string
		blockSyncOnly bool
	)

	cmd := &cobra.Command{
		Use:   "sync-test",
		Short: "Measure sync-to-tip speed on a Talis network",
		Long: `Spins up a fresh GCP instance, deploys celestia-appd, configures state sync
from the existing Talis validators, measures state sync + block sync to tip,
reports results, and tears down the instance.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			cfg.SSHPubKeyPath = resolveValue(sshPubKeyPath, EnvVarSSHKeyPath, cfg.SSHPubKeyPath)
			cfg.GoogleCloudProject = resolveValue(gcProject, EnvVarGoogleCloudProject, cfg.GoogleCloudProject)
			cfg.GoogleCloudKeyJSONPath = resolveValue(gcKeyJSONPath, EnvVarGoogleCloudKeyJSONPath, cfg.GoogleCloudKeyJSONPath)

			if cfg.GoogleCloudProject == "" {
				return fmt.Errorf("google cloud project is required (use --gc-project, env GOOGLE_CLOUD_PROJECT, or config)")
			}

			resolvedSSHKeyPath := resolveValue(sshKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))
			if resolvedSSHKeyPath == "" {
				return fmt.Errorf("SSH private key path is required (use --ssh-key-path or set ssh_pub_key_path in config)")
			}

			sshPubKey, err := os.ReadFile(cfg.SSHPubKeyPath)
			if err != nil {
				return fmt.Errorf("failed to read SSH public key at %s: %w", cfg.SSHPubKeyPath, err)
			}

			if binaryPath == "" {
				return fmt.Errorf("--binary-path is required (path to celestia-appd binary to deploy)")
			}
			if _, err := os.Stat(binaryPath); err != nil {
				return fmt.Errorf("binary not found at %s: %w", binaryPath, err)
			}

			opts, err := gcClientOptions(cfg)
			if err != nil {
				return fmt.Errorf("failed to create GCP client options: %w", err)
			}

			// Pick region
			if region == "" || region == RandomRegion {
				region = RandomGCRegion()
			}

			// Pick a random validator as RPC source
			validator := cfg.Validators[rand.Intn(len(cfg.Validators))]
			if validator.PublicIP == "" || validator.PublicIP == "TBD" {
				return fmt.Errorf("selected validator %s has no public IP", validator.Name)
			}
			rpcEndpoint := fmt.Sprintf("http://%s:26657", validator.PublicIP)
			log.Printf("Using validator %s (%s) as RPC source", validator.Name, validator.PublicIP)

			// Build peer list from all validators
			var peers []string
			for _, v := range cfg.Validators {
				if v.PublicIP == "" || v.PublicIP == "TBD" {
					continue
				}
				peers = append(peers, fmt.Sprintf("%s:26656", v.PublicIP))
			}

			// Create the sync test instance
			syncInst := Instance{
				NodeType: Validator,
				Name:     fmt.Sprintf("sync-test-%d", time.Now().Unix()),
				Provider: GoogleCloud,
				Slug:     syncTestMachineType,
				Region:   region,
				Tags:     []string{"talis", "sync-test"},
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			log.Printf("Creating sync-test instance in region %s...", region)
			created, err := CreateGCInstances(ctx, cfg.GoogleCloudProject, []Instance{syncInst}, string(sshPubKey), opts, 1)
			if err != nil {
				return fmt.Errorf("failed to create GCP instance: %w", err)
			}
			if len(created) == 0 {
				return fmt.Errorf("no instance was created")
			}

			inst := created[0]
			log.Printf("Instance %s created with IP %s", inst.Name, inst.PublicIP)

			// Setup cleanup on interrupt
			teardown := func() {
				if keep {
					log.Printf("--keep flag set, leaving instance %s (%s) running", inst.Name, inst.PublicIP)
					return
				}
				log.Printf("Tearing down instance %s...", inst.Name)
				teardownCtx, teardownCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer teardownCancel()
				destroyInst := inst
				destroyInst.Region = region
				if _, err := DestroyGCInstances(teardownCtx, cfg.GoogleCloudProject, []Instance{destroyInst}, opts, 1); err != nil {
					log.Printf("Warning: failed to destroy instance %s: %v", inst.Name, err)
				}
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				sig := <-sigCh
				log.Printf("Received signal %v, cleaning up...", sig)
				cancel() // Stop all in-flight operations (SSH loop, etc.)
				teardown()
				os.Exit(1)
			}()

			defer func() {
				signal.Stop(sigCh)
				teardown()
			}()

			// Wait for SSH to become available
			log.Printf("Waiting for SSH to become available on %s...", inst.PublicIP)
			log.Printf("  SSH private key: %s", resolvedSSHKeyPath)
			log.Printf("  SSH public key:  %s", cfg.SSHPubKeyPath)
			if err := waitForSSH(ctx, inst.PublicIP, resolvedSSHKeyPath, 2*time.Minute); err != nil {
				return fmt.Errorf("SSH not available: %w", err)
			}
			log.Printf("SSH is available")

			// SCP the binary to the instance
			log.Printf("Uploading celestia-appd binary to %s...", inst.PublicIP)
			if err := scpFile(ctx, binaryPath, inst.PublicIP, "/usr/local/bin/celestia-appd", resolvedSSHKeyPath); err != nil {
				return fmt.Errorf("failed to upload binary: %w", err)
			}
			log.Printf("Binary uploaded successfully")

			// Make binary executable
			if err := runSSHCommand(ctx, inst.PublicIP, resolvedSSHKeyPath, "chmod +x /usr/local/bin/celestia-appd"); err != nil {
				return fmt.Errorf("failed to chmod binary: %w", err)
			}

			// Run sync test iterations
			for i := 1; i <= iterations; i++ {
				if iterations > 1 {
					log.Printf("=== Starting iteration %d/%d ===", i, iterations)
				}

				script := buildSyncScript(cfg.ChainID, rpcEndpoint, peers, i, iterations, blockSyncOnly)
				log.Printf("Starting sync measurement on %s...", inst.PublicIP)

				if err := runSSHStreaming(ctx, inst.PublicIP, resolvedSSHKeyPath, script); err != nil {
					return fmt.Errorf("sync test failed on iteration %d: %w", i, err)
				}

				if i < iterations {
					log.Printf("Cooldown for %ds before next iteration...", cooldown)
					select {
					case <-time.After(time.Duration(cooldown) * time.Second):
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory with config.json")
	cmd.Flags().StringVarP(&sshPubKeyPath, "ssh-pub-key-path", "s", "", "path to SSH public key")
	cmd.Flags().StringVar(&sshKeyPath, "ssh-key-path", "", "path to SSH private key (default: derived from config's ssh_pub_key_path)")
	cmd.Flags().StringVar(&gcProject, "gc-project", "", "Google Cloud project")
	cmd.Flags().StringVar(&gcKeyJSONPath, "gc-key-json-path", "", "path to Google Cloud service account key JSON")
	cmd.Flags().StringVarP(&region, "region", "r", "random", "GCP region for the sync node")
	cmd.Flags().IntVarP(&iterations, "iterations", "n", 1, "number of sync iterations")
	cmd.Flags().IntVar(&cooldown, "cooldown", 30, "seconds between iterations")
	cmd.Flags().BoolVar(&keep, "keep", false, "don't tear down the instance after (for debugging)")
	cmd.Flags().BoolVar(&blockSyncOnly, "block-sync-only", false, "skip state sync and only block sync from genesis")
	cmd.Flags().StringVar(&binaryPath, "binary-path", "", "path to celestia-appd binary to deploy (required)")
	_ = cmd.MarkFlagRequired("binary-path")

	return cmd
}

// waitForSSH polls until an SSH connection succeeds or the timeout is reached.
func waitForSSH(ctx context.Context, ip, sshKeyPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	attempt := 0
	var lastErr error
	var lastOut string
	for time.Now().Before(deadline) {
		attempt++
		sshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ssh := exec.CommandContext(sshCtx,
			"ssh",
			"-i", sshKeyPath,
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "ConnectTimeout=5",
			fmt.Sprintf("root@%s", ip),
			"echo ok",
		)
		out, err := ssh.CombinedOutput()
		cancel()
		outStr := strings.TrimSpace(string(out))
		if err == nil && strings.Contains(outStr, "ok") {
			return nil
		}
		lastErr = err
		lastOut = outStr
		fmt.Fprintf(os.Stderr, "  SSH attempt %d: err=%v out=%q\n", attempt, err, truncateOutput(outStr, 200))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("SSH not available on %s after %v (%d attempts), last error: %v, last output: %s", ip, timeout, attempt, lastErr, truncateOutput(lastOut, 500))
}

func truncateOutput(s string, maxLen int) string {
	// Only keep the last maxLen characters for readability
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return "..." + s[len(s)-maxLen:]
	}
	return s
}

// scpFile copies a local file to a remote path via SCP.
func scpFile(ctx context.Context, localPath, ip, remotePath, sshKeyPath string) error {
	scp := exec.CommandContext(ctx,
		"scp",
		"-i", sshKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		localPath,
		fmt.Sprintf("root@%s:%s", ip, remotePath),
	)
	if out, err := scp.CombinedOutput(); err != nil {
		return fmt.Errorf("scp error: %v\n%s", err, out)
	}
	return nil
}

// runSSHCommand runs a command on a remote host via SSH and returns the error if any.
func runSSHCommand(ctx context.Context, ip, sshKeyPath, command string) error {
	ssh := exec.CommandContext(ctx,
		"ssh",
		"-i", sshKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", ip),
		command,
	)
	if out, err := ssh.CombinedOutput(); err != nil {
		return fmt.Errorf("ssh error: %v\n%s", err, out)
	}
	return nil
}

// runSSHStreaming runs a command on a remote host via SSH, streaming stdout/stderr
// directly to the user's terminal for real-time output.
func runSSHStreaming(ctx context.Context, ip, sshKeyPath, command string) error {
	ssh := exec.CommandContext(ctx,
		"ssh",
		"-i", sshKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=5",
		fmt.Sprintf("root@%s", ip),
		command,
	)
	ssh.Stdout = os.Stdout
	ssh.Stderr = os.Stderr
	return ssh.Run()
}

// buildSyncScript generates the shell script that runs on the remote instance to
// perform state sync configuration, start the node, and measure sync times.
func buildSyncScript(chainID, rpcEndpoint string, peerIPs []string, iteration, totalIterations int, blockSyncOnly bool) string {
	peersStr := strings.Join(peerIPs, ",")
	blockSyncOnlyStr := "false"
	if blockSyncOnly {
		blockSyncOnlyStr = "true"
	}

	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail

CHAIN_ID="%s"
RPC="%s"
PEERS="%s"
ITERATION=%d
TOTAL_ITERATIONS=%d
BLOCK_SYNC_ONLY=%s
HOME_DIR="/root/.celestia-app-sync"
POLL_INTERVAL=5
SYNC_TIMEOUT=7200

printf "\n=========================================\n"
printf "SYNC TEST - ITERATION %%d/%%d\n" "$ITERATION" "$TOTAL_ITERATIONS"
printf "=========================================\n"
printf "Chain ID:  %%s\n" "$CHAIN_ID"
printf "RPC:       %%s\n" "$RPC"
printf "=========================================\n\n"

# Install jq if not present
if ! command -v jq &>/dev/null; then
    echo "Installing jq..."
    apt-get update -qq && apt-get install -y -qq jq >/dev/null 2>&1
fi

# Clean up any previous run
rm -rf "$HOME_DIR"

echo "Initializing celestia-appd..."
celestia-appd init sync-node --chain-id "$CHAIN_ID" --home "$HOME_DIR" >/dev/null 2>&1

# Fetch genesis from validator RPC
echo "Fetching genesis from $RPC..."
for attempt in $(seq 1 5); do
    if curl -sf "$RPC/genesis" | jq '.result.genesis' > "$HOME_DIR/config/genesis.json" 2>/dev/null; then
        GENESIS_SIZE=$(wc -c < "$HOME_DIR/config/genesis.json")
        echo "Genesis saved ($GENESIS_SIZE bytes)"
        break
    fi
    echo "Attempt $attempt failed, retrying in 5s..."
    sleep 5
done

if [ ! -s "$HOME_DIR/config/genesis.json" ]; then
    echo "ERROR: Failed to fetch genesis"
    exit 1
fi

# Fetch node IDs and build persistent_peers
echo "Building peer list..."
PERSISTENT_PEERS=""
for peer_addr in $(echo "$PEERS" | tr ',' ' '); do
    PEER_IP=$(echo "$peer_addr" | cut -d: -f1)
    PEER_PORT=$(echo "$peer_addr" | cut -d: -f2)
    PEER_RPC="http://${PEER_IP}:26657"

    NODE_ID=$(curl -sf "$PEER_RPC/status" 2>/dev/null | jq -r '.result.node_info.id // empty' 2>/dev/null || true)
    if [ -n "$NODE_ID" ]; then
        if [ -n "$PERSISTENT_PEERS" ]; then
            PERSISTENT_PEERS="${PERSISTENT_PEERS},"
        fi
        PERSISTENT_PEERS="${PERSISTENT_PEERS}${NODE_ID}@${PEER_IP}:${PEER_PORT}"
        echo "  Added peer: ${NODE_ID}@${PEER_IP}:${PEER_PORT}"
    else
        echo "  Warning: could not get node ID for $PEER_IP"
    fi
done

if [ -z "$PERSISTENT_PEERS" ]; then
    echo "ERROR: No peers found"
    exit 1
fi

echo "Found $(echo "$PERSISTENT_PEERS" | tr ',' '\n' | wc -l | tr -d ' ') peers"

# Configure persistent peers
sed -i "s|^persistent_peers *=.*|persistent_peers = \"$PERSISTENT_PEERS\"|" "$HOME_DIR/config/config.toml"

# Disable block sync verification for faster sync
sed -i -E "s|^verify_data *=.*|verify_data = false|" "$HOME_DIR/config/config.toml"

# Query network for latest height
echo "Querying network for latest height..."
LATEST_HEIGHT=$(curl -sf "$RPC/block" | jq -r '.result.block.header.height')
echo "Latest height:  $LATEST_HEIGHT"

if [ "$BLOCK_SYNC_ONLY" = "true" ]; then
    echo "Block sync only mode: skipping state sync, syncing from genesis"
    BLOCK_HEIGHT=0
else
    BLOCK_HEIGHT=$((LATEST_HEIGHT - 1000))
    TRUST_HASH=$(curl -sf "$RPC/block?height=$BLOCK_HEIGHT" | jq -r '.result.block_id.hash')

    echo "Trust height:   $BLOCK_HEIGHT"
    echo "Trust hash:     $TRUST_HASH"

    # Enable state sync
    sed -i -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true|" "$HOME_DIR/config/config.toml"
    sed -i -E "s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"$RPC,$RPC\"|" "$HOME_DIR/config/config.toml"
    sed -i -E "s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1$BLOCK_HEIGHT|" "$HOME_DIR/config/config.toml"
    sed -i -E "s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"$TRUST_HASH\"|" "$HOME_DIR/config/config.toml"
fi

echo ""
echo "Starting celestia-appd..."
START_TIME=$(date +%%s)

celestia-appd start --home "$HOME_DIR" --force-no-bbr >/root/sync-node.log 2>&1 &
NODE_PID=$!

cleanup() {
    kill -TERM "$NODE_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Wait for RPC to be available
echo "Waiting for local RPC..."
for i in $(seq 1 60); do
    if curl -sf http://localhost:26657/status >/dev/null 2>&1; then
        echo "Local RPC is available"
        break
    fi
    sleep 2
done

if ! curl -sf http://localhost:26657/status >/dev/null 2>&1; then
    echo "ERROR: Local RPC not available after 120s"
    echo "Last 50 lines of log:"
    tail -50 /root/sync-node.log || true
    exit 1
fi

printf "\n=== Monitoring Sync Progress ===\n"
STATE_SYNC_COMPLETE=false
STATE_SYNC_END_TIME=""
PREV_HEIGHT=0
STALL_COUNT=0
MAX_STALLS=24

elapsed=0
while [ $elapsed -lt $SYNC_TIMEOUT ]; do
    # Check if process is still alive
    if ! kill -0 $NODE_PID 2>/dev/null; then
        echo "ERROR: celestia-appd process died"
        echo "Last 50 lines of log:"
        tail -50 /root/sync-node.log || true
        exit 1
    fi

    STATUS=$(curl -sf http://localhost:26657/status 2>/dev/null || echo "{}")
    CATCHING_UP=$(echo "$STATUS" | jq -r '.result.sync_info.catching_up // "true"')
    CURRENT_HEIGHT=$(echo "$STATUS" | jq -r '.result.sync_info.latest_block_height // "0"')
    NETWORK_TIP=$(curl -sf "$RPC/block" 2>/dev/null | jq -r '.result.block.header.height // "0"' 2>/dev/null || echo "0")
    BLOCKS_BEHIND=$((NETWORK_TIP - CURRENT_HEIGHT))
    [ $BLOCKS_BEHIND -lt 0 ] && BLOCKS_BEHIND=0

    # Detect stalled sync
    if [ "$CURRENT_HEIGHT" = "$PREV_HEIGHT" ] && [ "$CURRENT_HEIGHT" != "0" ] && [ "$BLOCKS_BEHIND" -gt "5" ]; then
        STALL_COUNT=$((STALL_COUNT + 1))
        if [ $STALL_COUNT -ge $MAX_STALLS ]; then
            NUM_PEERS=$(curl -sf http://localhost:26657/net_info 2>/dev/null | jq -r '.result.n_peers // "0"' 2>/dev/null || echo "0")
            echo "ERROR: Sync stalled for 2 minutes at height $CURRENT_HEIGHT"
            echo "Peers connected: $NUM_PEERS"
            echo "Last 50 lines of log:"
            tail -50 /root/sync-node.log || true
            exit 1
        fi
        echo "[$(date +%%T)] Height: $CURRENT_HEIGHT / $NETWORK_TIP (${BLOCKS_BEHIND} behind) | STALLED ($STALL_COUNT/${MAX_STALLS})"
    else
        STALL_COUNT=0
        echo "[$(date +%%T)] Height: $CURRENT_HEIGHT / $NETWORK_TIP (${BLOCKS_BEHIND} behind) | Catching up: $CATCHING_UP"
    fi
    PREV_HEIGHT=$CURRENT_HEIGHT

    # Check state sync completion
    if [ "$STATE_SYNC_COMPLETE" = "false" ] && [ "$CURRENT_HEIGHT" -ge "$BLOCK_HEIGHT" ] 2>/dev/null; then
        STATE_SYNC_END_TIME=$(date +%%s)
        STATE_SYNC_DURATION=$((STATE_SYNC_END_TIME - START_TIME))
        printf "\nState sync complete! Reached trust height %%s (%%ss)\n=== Monitoring Block Sync to Tip ===\n" "$BLOCK_HEIGHT" "$STATE_SYNC_DURATION"
        STATE_SYNC_COMPLETE=true
    fi

    # Check if fully synced
    if [ "$BLOCKS_BEHIND" -le "0" ] 2>/dev/null; then
        TOTAL_END_TIME=$(date +%%s)
        TOTAL_DURATION=$((TOTAL_END_TIME - START_TIME))
        BLOCK_SYNC_DURATION=$((TOTAL_END_TIME - ${STATE_SYNC_END_TIME:-$START_TIME}))

        if [ -z "${STATE_SYNC_END_TIME:-}" ]; then
            STATE_SYNC_DURATION=$TOTAL_DURATION
        fi

        printf "\n=========================================\n"
        printf "ITERATION %%d/%%d COMPLETE\n" "$ITERATION" "$TOTAL_ITERATIONS"
        printf "=========================================\n"
        printf "State sync duration:      %%ss\n" "${STATE_SYNC_DURATION:-$TOTAL_DURATION}"
        printf "Block sync duration:      %%ss\n" "$BLOCK_SYNC_DURATION"
        printf "Total sync duration:      %%ss\n" "$TOTAL_DURATION"
        printf "Final height:             %%s\n" "$CURRENT_HEIGHT"
        printf "Network tip:              %%s\n" "$NETWORK_TIP"
        printf "=========================================\n"

        kill -TERM "$NODE_PID" 2>/dev/null || true
        trap - EXIT INT TERM
        exit 0
    fi

    sleep $POLL_INTERVAL
    elapsed=$((elapsed + POLL_INTERVAL))
done

echo "ERROR: Sync timeout (${SYNC_TIMEOUT}s)"
exit 1
`, chainID, rpcEndpoint, peersStr, iteration, totalIterations, blockSyncOnlyStr)
}
