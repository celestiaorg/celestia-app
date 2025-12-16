# Metrics Package

This package provides a metrics collection infrastructure for Celestia nodes, consisting of:

1. **metrics-server** - A gRPC service that manages Prometheus scrape targets
2. **Prometheus** - Time-series database for metrics storage
3. **Grafana** - Dashboard visualization

## Architecture

```
┌─────────────────┐     gRPC (register)      ┌─────────────────┐
│  Celestia Node  │ ──────────────────────►  │  Metrics Server │
│  (port 26660)   │                          │  (port 9900)    │
└─────────────────┘                          └────────┬────────┘
                                                      │
                                                      │ writes
                                                      ▼
┌─────────────────┐     file_sd_configs      ┌─────────────────┐
│   Prometheus    │ ◄─────────────────────── │  targets.json   │
│  (internal)     │                          └─────────────────┘
└────────┬────────┘
         │ data source
         ▼
┌─────────────────┐
│    Grafana      │ ◄──── Admin access (port 3000)
│  (exposed)      │
└─────────────────┘
```

## Quick Start

### 1. Start the metrics stack

```bash
cd metrics/docker
docker-compose up -d
```

This starts:
- **metrics-server** on port `9900` (gRPC for node registration)
- **Prometheus** (internal, not exposed)
- **Grafana** on port `3000` (dashboards)

### 2. Enable Prometheus on Celestia nodes

When initializing a Talis network, use the `--prometheus` flag:

```bash
talis init --chainID my-chain --experiment test --prometheus
```

This enables the Prometheus metrics endpoint on port `26660` for all nodes.

### 3. Register nodes with the metrics server

#### Register all nodes from a Talis deployment

```bash
talis metrics register-all --server localhost:9900 --directory /path/to/talis
```

#### Register a single node

```bash
talis metrics register \
  --server localhost:9900 \
  --node-id validator-0 \
  --address 10.0.0.1:26660 \
  --label chain_id=my-chain \
  --label role=validator
```

### 4. View dashboards

Open Grafana at http://localhost:3000

- Default credentials: `admin` / `admin` (or set via `GRAFANA_PASSWORD` env var)
- Pre-configured dashboard: **Celestia Network**

## Commands

### Talis Metrics Commands

```bash
# Register all nodes from deployment
talis metrics register-all -s localhost:9900 -d /path/to/talis

# Register a single node
talis metrics register -s localhost:9900 -n validator-0 -a 10.0.0.1:26660

# Deregister a node
talis metrics deregister -s localhost:9900 -n validator-0

# List all registered nodes
talis metrics list -s localhost:9900
```

### Direct gRPC (using grpcurl)

```bash
# Register a node
grpcurl -plaintext -d '{
  "node_id": "validator-0",
  "address": "10.0.0.1:26660",
  "labels": {"chain_id": "my-chain", "role": "validator"}
}' localhost:9900 metrics.v1.Registry/Register

# List all targets
grpcurl -plaintext localhost:9900 metrics.v1.Registry/ListTargets
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GRAFANA_PASSWORD` | `admin` | Grafana admin password |

### Prometheus Scrape Interval

Edit `metrics/docker/prometheus/prometheus.yml` to adjust scrape settings:

```yaml
global:
  scrape_interval: 15s  # How often to scrape targets

scrape_configs:
  - job_name: 'celestia-nodes'
    file_sd_configs:
      - files:
          - /targets/targets.json
        refresh_interval: 10s  # How often to check for new targets
```

## Go Client Library

The `metrics/client` package provides a Go client for programmatic registration:

```go
import "github.com/celestiaorg/celestia-app/v6/metrics/client"

// Connect to metrics server
c, err := client.New("localhost:9900")
if err != nil {
    log.Fatal(err)
}
defer c.Close()

// Register a node
err = c.Register(ctx, "validator-0", "10.0.0.1:26660", map[string]string{
    "chain_id": "my-chain",
    "role": "validator",
})

// List all targets
targets, err := c.ListTargets(ctx)
```

## Security

- **Prometheus** is internal only (no exposed ports)
- **Grafana** requires authentication (port 3000)
- **metrics-server** gRPC is exposed (port 9900) - consider mTLS for production

## Development

### Build metrics-server binary

```bash
go build ./metrics/cmd/metrics-server
```

### Generate proto code

```bash
cd metrics/proto
buf generate
```

### Run locally without Docker

```bash
# Start metrics-server
./metrics-server --port 9900 --targets-file ./targets.json

# Start Prometheus (pointing to targets.json)
prometheus --config.file=metrics/docker/prometheus/prometheus.yml

# Start Grafana
# (requires provisioning setup)
```

## Dashboard Panels

The pre-configured Celestia dashboard includes:

### Overview
- Block Height
- Validators
- Connected Peers
- Mempool Size

### Consensus
- Block Rate (blocks/min)
- Consensus Rounds
- Block Interval
- Block Size

### Mempool
- Mempool Size
- Mempool Size (bytes)

### P2P Network
- Connected Peers
- P2P Bandwidth (send/recv)
