# Metrics Package

This package provides a simple metrics stack for Celestia nodes using Prometheus and Grafana with file-based target discovery.

## Architecture

```
┌─────────────────┐   scrape (26660)   ┌─────────────────┐
│  Celestia Node  │ ─────────────────► │   Prometheus    │
│  (port 26660)   │                    │  (port 9090)    │
└─────────────────┘                    └────────┬────────┘
                                                │ data source
                                                ▼
                                          ┌─────────────┐
                                          │   Grafana   │
                                          │ (port 3000) │
                                          └─────────────┘
```

Prometheus discovers targets via a local `targets.json` file mounted into the container (`file_sd_configs`).

## Quick Start

### 1. Enable Prometheus on Celestia nodes

When initializing a Talis network, use the `--prometheus` flag:

```bash
talis init --chainID my-chain --experiment test --prometheus
```

This enables the Prometheus metrics endpoint on port `26660` for all nodes.

### 2. Generate targets.json

#### Option A: From Talis (recommended)

```bash
talis metrics export-targets \
  --directory /path/to/talis \
  --output metrics/docker/targets/targets.json
```

#### Option B: Manual (standalone)

Edit `metrics/docker/targets/targets.json` to include your nodes:

```json
[
  {
    "targets": ["10.0.0.1:26660"],
    "labels": {
      "chain_id": "my-chain",
      "experiment": "experiment-1",
      "role": "validator",
      "region": "us-east-1",
      "provider": "manual",
      "node_id": "validator-0"
    }
  }
]
```

### 3. Start the metrics stack

```bash
cd metrics/docker
docker compose up -d
```

This starts:
- **Prometheus** on port `9090`
- **Grafana** on port `3000`

### Optional: use helper scripts

```bash
# Install Docker + Compose on a fresh Ubuntu host
./metrics/install_prereqs.sh

# Start Prometheus + Grafana from the bundled docker compose config
./metrics/start_metrics.sh
```

### 4. View dashboards

Open Grafana at http://localhost:3000

- Default credentials: `admin` / `admin` (or set via `GRAFANA_PASSWORD` env var)
- Pre-configured dashboard: **Celestia Network**

## Talis Command

```bash
# Export targets from a Talis deployment
# (use --address-source private for internal networks)
talis metrics export-targets -d /path/to/talis -o ./targets.json
```

Flags:
- `--address-source` (default: public) selects public or private IPs
- `--port` (default: 26660) selects the metrics port
- `--pretty` pretty-prints JSON output

## Talis Metrics Node

If you add a metrics node with `talis add -t metrics`, `talis genesis` will stage the metrics payload (docker config, scripts, and generated targets) and `talis deploy` will install and start the stack on that node automatically.

## Configuration

### Prometheus Scrape Interval

Edit `metrics/docker/prometheus/prometheus.yml` to adjust scrape settings:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'celestia-nodes'
    file_sd_configs:
      - files:
          - /targets/targets.json
        refresh_interval: 30s
```

## Security

- **Prometheus** is exposed on port `9090` by default; remove the port mapping in `metrics/docker/docker-compose.yml` if you want it internal only.
- **Grafana** requires authentication (port 3000)

## Updating Targets

Edit `metrics/docker/targets/targets.json` and Prometheus will pick up changes within `refresh_interval` (default: 30s).
