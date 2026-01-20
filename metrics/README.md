# Metrics Package

This package provides a metrics stack for Consensus nodes using Prometheus and Grafana with file-based target discovery, plus a Loki endpoint for log ingestion.

## Architecture

```text
┌─────────────────┐   scrape (26660)   ┌─────────────────┐
│ Consensus Node  │ ─────────────────► │   Prometheus    │
│  (port 26660)   │                    │  (internal)     │
└─────────────────┘                    └────────┬────────┘
                                               │ data source
                                               ▼
                                         ┌─────────────┐
                                         │   Grafana   │
                                         │ (port 3000) │
                                         └─────────────┘

┌──────────────────────┐    push logs    ┌─────────────┐
│ latency-monitor logs │ ──────────────► │    Loki     │
└──────────────────────┘                 │ (port 3100) │
                                         └─────────────┘
```

Prometheus discovers targets via a local `targets.json` file mounted into the container (`file_sd_configs`).

## Quick Start with Talis

### 1. Initialize with metrics enabled

```bash
talis init --chainID my-chain --experiment test --with-metrics
```

This:

- Adds a metrics node to the configuration
- Enables Prometheus metrics endpoint (port 26660) on all validator nodes

### 2. Add validators and provision

```bash
talis add -t validator -c 10
talis up
```

### 3. Generate payload with metrics

```bash
talis genesis --metrics-dir /path/to/celestia-app/metrics -b build
```

The `--metrics-dir` flag points to this directory. During genesis, Talis:

- Copies the docker-compose stack and scripts to the payload
- Generates `targets.json` from the configured validator IPs

### 4. Deploy

```bash
talis deploy
```

After deployment completes, Talis prints the Grafana URL and credentials:

```text
Grafana available at:
  http://<metrics-node-ip>:3000  (credentials: admin/<random-password>)
```

## Helper Scripts

```bash
# Install Docker + Compose on a fresh Ubuntu host
./metrics/install_metrics.sh

# Start Prometheus + Grafana from the bundled docker compose config
./metrics/start_metrics.sh
```

## Configuration

### Prometheus Scrape Interval

Edit `metrics/docker/prometheus/prometheus.yml` to adjust scrape settings:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'consensus-nodes'
    file_sd_configs:
      - files:
          - /targets/targets.json
        refresh_interval: 30s
```

### Grafana Password

Set via environment variable before starting:

```bash
GRAFANA_PASSWORD=mysecretpassword docker compose up -d
```

When using Talis, a random password is generated automatically during `talis genesis`.

### Loki + Promtail (latency-monitor logs)

The Loki container is included in the stack and a Grafana Loki datasource is pre-provisioned.

Start latency-monitor with Loki enabled:

```bash
talis latency-monitor --instances 1 --loki-url http://<metrics-node-ip>:3100
```

## Security

- **Prometheus** is internal only (not exposed to the network); Grafana accesses it via Docker's internal network
- **Grafana** requires authentication (port 3000)
- **Loki** is exposed on port 3100 for log ingestion

## Checking Status

From the `metrics/docker` directory:

```bash
# Check container state
docker ps

# View logs
docker compose logs -f

# Restart the stack
docker compose restart

# Stop the stack
docker compose down
```

## Updating Targets

Edit `metrics/docker/targets/targets.json` and Prometheus will pick up changes within `refresh_interval` (default: 30s).
