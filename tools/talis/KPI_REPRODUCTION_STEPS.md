# Celestia KPI Reproduction Steps

This document provides instructions for reproducing the core-app KPIs. These KPIs measure transaction submission performance and sync-to-tip capabilities.

## Prerequisites

### Local Machine Setup

1. **Verify block time configuration for 32MB/3sec blocks:**

   Make sure app is configured for the target throughput. Verify that `DelayedPrecommitTimeout` is set to 2800ms for 3s block time.

2. **Install celestia-app and dependencies:**

   ```bash
   # Build all necessary binaries (must be done after verifying DelayedPrecommitTimeout)
   make build-talis-bins

   # Install talis
   go install ./tools/talis/
   ```

3. **Set up cloud provider credentials:**

Google Cloud is recommended for high-throughput tests. Ask the DevOps team for access to Celestia's Google Cloud fibreda workspace.

   ```bash
   # Create a .env file
   talis init-env --provider googlecloud

   # Fill in the .env file with your credentials:
   GOOGLE_CLOUD_PROJECT="fibreda"
   GOOGLE_CLOUD_KEY_JSON_PATH="/path/to/service-account-key.json"
   ```

1. **SSH key is required for running experiments:**

   Create a new SSH key or use existing one.

   For Google Cloud:
   - The SSH key is automatically added to instance metadata by talis
   - Ensure your service account has the necessary permissions to create instances

   Configure these variables in `.env`:

   ```
   # SSH Configuration (optional - will use defaults if not set)

  TALIS_SSH_KEY_PATH=your-key-path
  TALIS_SSH_KEY_NAME=your-key-name

   ```

## Transaction Submission KPIs

These KPIs measure the network's ability to handle high-throughput blob submissions with target metrics:

- **Throughput:** 8MB/1sec (32MB/3sec block time)
- **Success Rate:** 99.9%
- **Average user Latency:** ≤8 seconds

### 1. Initialize Talis Network

```bash
# Initialize with observability for metrics collection
talis init -c kpi-test-chain -e tx-kpi --with-observability --provider googlecloud

# Add validator nodes (50-100 validators recommended for realistic network)
talis add -t validator -c 50 --provider googlecloud

# Note: talis automatically configures mempool, consensus timeouts, and gRPC
# The default configs are optimized for high-throughput testing
```

### 2. Deploy Network

```bash
# Spin up cloud instances (specify SSH key if not using defaults)
talis up --provider googlecloud --workers 20

# For DigitalOcean (if you need to specify SSH key):
# talis up -n <your-ssh-key-name> -s ~/.ssh/id_ed25519_no_passphrase --workers 20

# Create genesis with appropriate square size
# Square size 256 allows for ~32MB blocks
talis genesis -s 256 -b ./build

# Deploy the network (specify SSH key if needed)
talis deploy --direct-payload-upload --workers 20

# After deployment completes, talis will output the Grafana access information:
      #  URL, credentials.                       

# Wait for network to start and optionally confirm all validators are online
talis status
```

### 3. Run Transaction Submission Tests

**NOTE** Reset the network between KPI experiments for fresh state/accurate results.

```bash
talis reset
talis deploy -w 20
```

#### Test 1: Baseline 8MB/1sec (Single Submitter)

**Target:** One latency monitor submitting 8MB blobs every second

```bash
talis latency-monitor -i 1 -b 8000000 -z 8000000 -s 1000ms

# Let run for at least 15-30 minutes to collect sufficient data
```

**Expected Results:**

- Success rate: >=99.9%
- Average user latency: 6-8 seconds
- Zero or minimal failures
- No Evictions

#### Test 2: Load Shedding (Two Submitters, 8MB/1sec each)

**Target:** Two latency monitors submitting 8MB blobs every second (total 16MB/1sec)

```bash
# Terminal 1: SSH to validator-0
talis latency-monitor -i 1 -b 8000000 -z 8000000 -s 1000ms
# Run both simultaneously for 15-30 minutes
```

**Expected Observations:**

- Gas price increases under load
- Some broadcast failures due to full mempool
- Higher latency due to eviction timeouts
- Sequence mismatch errors from resubmission race conditions
- Network attempts load shedding by evicting low fee transactions

#### Test 3: Parallel Submission (Multi-Worker)

**Target:** Single latency monitor with multiple parallel workers submitting 8MB total per second

```bash
talis latency-monitor --instances 1 -w 15 -b 8000000 -z 2000000 --submission-delay 100ms                            
# Run for 15-30 minutes
# 15 workers submitting 2-8MB txs every 100ms
```

**Expected Results:**

- Consistent throughput >9MB/1sec
- Good mempool distribution
- Ability to reliably fill blocks to capacity

#### Test 4: No Eviction (Optimal Conditions)

This can already be measured in the first experiment but if you have to re-run:

```bash
talis latency-monitor -i 1 -b 8000000 -z 8000000 -s 1000ms

# Let run for at least 15-30 minutes to collect sufficient data
```

**Expected Results:**

- Transactions included with zero evictions
- Zero or very little failures
- Latency in expected range (6-8 seconds)

### 4. Collect Metrics and Results

#### From Grafana

At `http://<observability-instance-ip>:3000` as displayed during `talis deploy`:

- Access celestia grafana dashboards displaying network data
- Access Latency monitor dashboards displaying submission statistics and latency monitor logs

## Cleanup

### Tear Down Talis Network

```bash
# Destroy cloud instances
talis down --workers 20
```

## Sync to Tip KPIs

These KPIs measure how quickly a new node can sync to the network tip using state sync and block sync.

**Target:** Total sync time <10 minutes (state sync + block sync)

### Running Sync Tests

#### Option 1: Local node (Mocha Testnet)

This script runs multiple iterations and provides statistical analysis:

```bash
# Single iteration
./scripts/mocha-measure-tip-sync.sh

# Multiple iterations with statistics (20 iterations recommended)
./scripts/mocha-measure-tip-sync.sh --iterations 20 --cooldown 30
```

#### Option 2: Manual Testing on DigitalOcean

For production-like testing on cloud infrastructure:

**1. Create a DigitalOcean droplet:**

```bash
# Recommended: 8GB RAM, 4 vCPUs (c-4 instance)
# Ubuntu 22.04 LTS
# Add your SSH key
```

**2. Set up the instance:**

```bash
# SSH into the droplet
ssh root@<droplet-ip>

# Install dependencies
apt-get update
apt-get install git build-essential curl jq -y

# Install Go
snap install go --channel=1.23/stable --classic
echo 'export GOPATH="$HOME/go"' >> ~/.profile
echo 'export GOBIN="$GOPATH/bin"' >> ~/.profile
echo 'export PATH="$GOBIN:$PATH"' >> ~/.profile
source ~/.profile

# Clone and build celestia-app
git clone https://github.com/celestiaorg/celestia-app.git
cd celestia-app
git checkout main  # or specific version tag
make install
```

**3. Run sync test:**

```bash
# For Mocha testnet
./scripts/mocha-measure-tip-sync.sh --iterations 20 --cooldown 30

# For Mainnet (use with caution, will take longer)
./scripts/block-sync.sh --network mainnet
```

**4. Monitor progress:**

```bash
# In another terminal, watch the sync progress
watch -n 5 'celestia-appd status | jq .sync_info'

# Check logs
tail -f ~/.celestia-app/node.log
```

### Analyzing Sync Results

The sync script provides output similar to this:

```
=========================================
FINAL STATISTICS (20 iterations with 30 second cooldowns)
=========================================

State Sync:
  Min:       139s
  Max:       180s
  Average:   155s
  Variance:  151

Block Sync:
  Min:       41s
  Max:       124s
  Average:   73s
  Variance:  516

Total Sync:
  Min:       180s
  Max:       304s
  Average:   228s
  Variance:  1204
=========================================
```

**KPI Outcome:**

- PASS if Total Sync Average <600s (10 minutes)
- FAIL if Total Sync Average ≥600s