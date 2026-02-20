# Arabica Consensus Node

Docker Compose setup for running an [Arabica testnet](https://docs.celestia.org/how-to-guides/arabica-devnet) consensus node with Prometheus and Grafana monitoring.

Unlike the Mocha setup which downloads a pre-built binary, this setup builds `celestia-appd` from source (standalone) using the local repository.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)

## Quick Start

```bash
cd docker/arabica
docker compose up -d
```

The node uses state sync to quickly catch up to the current chain height rather than syncing from genesis. Initial state sync typically takes a few minutes.

## Services

| Service | Description | URL |
| --- | --- | --- |
| celestia-appd | Arabica consensus node | <http://localhost:26657> (RPC) |
| prometheus | Metrics collection | <http://localhost:9090> |
| grafana | Metrics dashboards | <http://localhost:3000> |

## Monitoring with Grafana

1. Open <http://localhost:3000> in your browser.
1. Log in with username `admin` and password `admin` (or the value of `GRAFANA_PASSWORD` if set).
1. The home dashboard is pre-configured to the Celestia dashboard which displays consensus metrics, mempool stats, p2p info, and more.

## Monitoring Sync Progress

```bash
# Follow the node logs
docker compose logs -f celestia-appd

# Check sync status via RPC
curl -s localhost:26657/status | jq '.result.sync_info'
```

The node is fully synced when `catching_up` is `false`.

## Configuration

### Custom Grafana Password

```bash
GRAFANA_PASSWORD=my-secret-password docker compose up -d
```

### Data Persistence

Node data, Prometheus metrics, and Grafana state are stored in Docker volumes (`celestia-data`, `prometheus-data`, `grafana-data`). These persist across restarts. To reset all data:

```bash
docker compose down -v
```

## Stopping

```bash
# Stop all services (data is preserved)
docker compose down

# Stop and delete all data
docker compose down -v
```
