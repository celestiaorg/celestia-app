# talis-setup.sh

End-to-end automation script for deploying a Celestia fibre experiment using [talis](../). It runs every step from network initialization through load generation in a single invocation.

## Prerequisites

1. **talis binary** in `PATH`:

   ```sh
   go install github.com/celestiaorg/celestia-app/v8/tools/talis@latest
   ```

2. **Environment variables** (all required):

   | Variable                  | Description                          |
   |---------------------------|--------------------------------------|
   | `DIGITALOCEAN_TOKEN`      | DigitalOcean API token               |
   | `TALIS_SSH_KEY_PATH`      | Path to SSH private key              |
   | `TALIS_SSH_PUB_KEY_PATH`  | Path to SSH public key               |
   | `AWS_ACCESS_KEY_ID`       | AWS access key (for S3 payload upload) |
   | `AWS_SECRET_ACCESS_KEY`   | AWS secret key                       |
   | `AWS_S3_BUCKET`           | S3 bucket name                       |
   | `AWS_S3_ENDPOINT`         | S3 endpoint URL                      |
   | `AWS_DEFAULT_REGION`      | AWS region                           |

## Usage

```sh
./tools/talis/scripts/talis-setup.sh [options] [step ...]
```

### Options

| Flag      | Default | Description                                      |
|-----------|---------|--------------------------------------------------|
| `-n NAME` | `test`  | Network name (also settable via `NETWORK_NAME`)  |
| `-v NUM`  | `4`     | Number of validators (also settable via `NUM_VALIDATORS`) |

### Steps

When no steps are specified, all steps run in order (except `down`):

| Step           | Description                                           |
|----------------|-------------------------------------------------------|
| `init`         | Initialize talis network with observability enabled   |
| `add`          | Add validator nodes in the `sfo2` region              |
| `build`        | Build binaries via `make build-talis-bins` (skips if already built) |
| `up`           | Provision cloud VMs                                   |
| `genesis`      | Generate genesis, configs, and observability payload  |
| `deploy`       | Deploy binaries and configs to all nodes              |
| `setup-fibre`  | Register fibre host addresses and fund escrow accounts|
| `start-fibre`  | Start fibre servers on validators                     |
| `txsim`        | Start fibre-txsim load generators (2 instances, 4 concurrent each) |
| `down`         | Tear down VMs (not included in default "run all")     |

### Examples

```sh
# Run the full pipeline with defaults (4 validators)
./tools/talis/scripts/talis-setup.sh

# Custom network name and 8 validators
./tools/talis/scripts/talis-setup.sh -n my-experiment -v 8

# Run only specific steps
./tools/talis/scripts/talis-setup.sh deploy setup-fibre start-fibre

# Tear down when done
./tools/talis/scripts/talis-setup.sh down
```

## What it does

The script resolves the repository root automatically (it lives in `tools/talis/scripts/`), sets up the working directory at `<repo-root>/talis-setup/`, and delegates each step to the `talis` CLI with appropriate flags. Observability (Prometheus, Grafana, OTel Collector, Tempo) is enabled by default via `--with-observability`.

Binaries are built once via `make build-talis-bins` and cached in `talis-setup/build/`. Delete that directory to force a rebuild.
