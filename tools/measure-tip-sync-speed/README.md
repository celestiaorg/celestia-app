# measure-tip-sync-speed

Measures Celestia Mocha testnet sync-to-tip speed by spinning up a full node on Digital Ocean.

## Prerequisites

1. **Digital Ocean API token**

   ```bash
   export DIGITALOCEAN_TOKEN='your-token'
   ```

2. **SSH key uploaded to Digital Ocean**

   Upload your public key at: <https://cloud.digitalocean.com/account/security>

3. **Install the tool**

   ```bash
   go install ./tools/measure-tip-sync-speed
   ```

## Usage

```bash
# Required: specify your SSH private key
go run ./tools/measure-tip-sync-speed -k ~/.ssh/id_ed25519

# Multiple iterations + cooldown
go run ./tools/measure-tip-sync-speed -k ~/.ssh/id_ed25519 -n 20 -c 30

# Test specific branch
go run ./tools/measure-tip-sync-speed -k ~/.ssh/id_ed25519 -b my-branch
```

## Flags

| Flag | Description |
| ---- | ----------- |
| `-k, --ssh-key-path` | SSH private key path **(required)** |
| `-n, --iterations` | Number of sync iterations (default: 1) |
| `-c, --cooldown` | Seconds between iterations (default: 30) |
| `-b, --branch` | Git branch to test |
| `-s, --skip-build` | Skip building celestia-appd |
| `--no-cleanup` | Keep droplet alive |

## What It Does

1. Matches your SSH key with Digital Ocean
2. Creates Ubuntu droplet (8 vCPU, 16GB RAM, NYC)
3. Installs dependencies and builds celestia-appd
4. Runs `scripts/mocha-measure-tip-sync.sh`
5. Cleans up droplet

Takes ~5-10 minutes depending on the number of iterations.
