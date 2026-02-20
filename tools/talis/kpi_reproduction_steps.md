# Celestia KPI Reproduction Steps

This document provides instructions for reproducing the core-app KPIs. These KPIs measure transaction submission performance and sync to tip duration.

## Prerequisites

1. **Verify block time configuration for 32MB/3sec blocks:**

   Modify `app_consts.go` and set `DelayedPrecommitTimeout = time.Millisecond * 2800` for 3s block time.

2. **Install celestia-app and dependencies:**

   ```bash
   # Build all necessary binaries (must be done after modifying DelayedPrecommitTimeout)
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

4. **SSH key is required for running experiments:**

   Create a new SSH key or use existing one. For Google Cloud the SSH key is automatically added to instance metadata by talis.

   Configure these variables in `.env`:

   ```bash
   TALIS_SSH_KEY_PATH=your-key-path
   TALIS_SSH_KEY_NAME=your-key-name
   ```

5. **S3 bucket for faster deployment (optional):**

   For faster deployments using S3 upload instead of direct payload upload, configure an S3 bucket:

   ```bash
   AWS_ACCESS_KEY_ID=your-access-key
   AWS_SECRET_ACCESS_KEY=your-secret-key
   AWS_DEFAULT_REGION=fra1
   AWS_S3_ENDPOINT=https://fra1.digitaloceanspaces.com
   AWS_S3_BUCKET=your-bucket-name
   ```

## Talis Network Deployment

1. **Initialize Talis Network**

   ```bash
   # Initialize with observability for metrics collection
   talis init -c kpi-test-chain -e tx-kpi --with-observability --provider googlecloud

   # Add validator nodes (50-100 validators recommended for realistic network)
   talis add -t validator -c 50 --provider googlecloud
   ```

2. **Deploy Network**

   ```bash
   # Spin up cloud instances (specify SSH key if not using defaults)
   talis up --provider googlecloud --workers 20

   # Create genesis with appropriate square size
   # Square size 256 allows for ~32MB blocks
   talis genesis -s 256 -b ./build

   # Deploy the network (specify SSH key if needed)
   # Note: For faster deployment, use S3 upload instead of direct payload upload instead of --direct-payload-upload:
   talis deploy --workers 20

   # After deployment completes, talis will output the Grafana access information:
   # URL, credentials.

   # Wait for network to start and optionally confirm all validators are online
   talis status
   ```

## Transaction Submission KPIs

**NOTE** Reset the network between KPI experiments for fresh state/accurate results.

```bash
talis reset
talis deploy --workers 20
```

### KPI 1: 8MB/1sec (Single Submitter)

**Target:** One latency monitor submitting 8MB blobs every second

```bash
talis latency-monitor -i 1 -b 8000000 -z 8000000 -s 1000ms
```

**Expected Results:**

- Success rate: >=99.9%
- Average user latency: 6-8 seconds
- No Evictions

### KPI 2: Load Shedding (Two Submitters, 8MB/1sec each)

**Target:** Two latency monitors submitting 8MB blobs every second (total 16MB/1sec)

```bash
talis latency-monitor -i 2 -b 8000000 -z 8000000 -s 1000ms
```

**Expected Observations:**

- Gas price increases under load
- Some broadcast failures due to full mempool
- Higher latency due to eviction timeouts
- Sequence mismatch errors from resubmission race conditions
- Network attempts load shedding by evicting low fee transactions

### Test 3: Parallel Submission (Multiple Workers)

**Target:** Single latency monitor with multiple parallel workers trying to fill up the throughput.

```bash
# example: 15 workers submitting 2-8MB txs every 100ms
talis latency-monitor --instances 1 -w 15 -b 8000000 -z 2000000 --submission-delay 100ms                            
```

**Expected Results:**

- Consistent throughput >9MB/1sec
- Good mempool distribution

### Test 4: No Eviction (Optimal Conditions)

This can already be measured in the first experiment but if you have to re-run:

```bash
talis latency-monitor -i 1 -b 8000000 -z 8000000 -s 1000ms
```

**Expected Results:**

- Transactions included with zero evictions

## Collect Metrics and Results

### From Grafana

At `http://<observability-instance-ip>:3000` as displayed during `talis deploy`:

- Access celestia grafana dashboards displaying network data
- Access Latency monitor dashboards displaying submission statistics and latency monitor logs

## Cleanup

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

# Multiple iterations (20 iterations with 30s cooldown)
./scripts/mocha-measure-tip-sync.sh --iterations 20 --cooldown 30
```

#### Option 2: Cloud Testing on DigitalOcean

Use the `measure-tip-sync` tool which automates droplet creation, node setup, and sync measurement:

1. **Install the tool**

   ```bash
   go install ./tools/measure-tip-sync
   ```

1. **Running Tests:**

```bash
# Multiple iterations (20 iterations with 30s cooldown between each)
measure-tip-sync -k ~/.ssh/id_ed25519 -n 20 -c 30
```

### Analyzing Sync Results

The combined sync (state + block sync) must take less than 10 minutes.
