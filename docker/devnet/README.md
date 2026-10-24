# Celestia devnet

Two containers run one validator with Fibre and one bridge. The images contain fixed public test keys and require no shared credential volume. Never use these keys for real funds.

## Published images

Download `compose.yaml`, set `DEVNET_VERSION` to an app release tag that includes the devnet images, and run:

```sh
docker compose -f compose.yaml up -d --wait
docker compose -f compose.yaml logs -f
```

Images are `ghcr.io/celestiaorg/celestia-devnet-validator:<app-tag>` and `ghcr.io/celestiaorg/celestia-devnet-bridge:<app-tag>`.

The `devnet` workflow tests both images on amd64 and arm64, then publishes them for app release tags. To rerun publication, open **Actions → devnet → Run workflow** and set `tag` to the existing app release tag containing the devnet implementation.

## Build from source

From the repository root:

```sh
export DEVNET_VERSION=local
docker compose -f docker/devnet/compose.yaml -f docker/devnet/compose.build.yaml build
docker compose -f docker/devnet/compose.yaml up -d --wait
bash docker/devnet/smoke.sh
```

The smoke suite uses isolated project names and dynamically allocated loopback ports. It removes its own volumes and saves logs under `/tmp/celestia-devnet-test-<pid>`; set `DEVNET_LOG_DIR` to choose another directory.

## Connections and settings

| Service | Host endpoint |
| ------- | ------------- |
| Validator RPC | `http://localhost:26657` |
| Validator REST | `http://localhost:1317` |
| Validator gRPC | `localhost:9090` |
| Fibre | `localhost:7980` |
| Bridge RPC (no authentication) | `http://localhost:26658` |
| Bridge P2P (TCP) | `localhost:2121` |

Published ports bind to loopback. Validator signing stays inside the validator container.

| Environment variable | Default | Purpose |
| -------------------- | ------- | ------- |
| `P2P_NETWORK` | `devnet` | Network and chain ID; reset volumes when changing it |
| `BLOCK_TIME` | `1s` | Delayed precommit timeout |
| `FIBRE_HOST` | `localhost:7980` | Advertised Fibre address |
| `CORE_HOST` | `validator` | Bridge connection to the validator |
| `CORE_RPC_PORT` | `26657` | Bridge bootstrap RPC port |
| `CORE_GRPC_PORT` | `9090` | Bridge core gRPC port |

Container clients on the Compose network use `validator:26657`, `validator:9090`, and `bridge:26658`. Set `FIBRE_HOST=validator:7980` for these clients; host clients need an advertised address reachable from the host instead. Custom core endpoints must serve the same chain. `BLOCK_TIME` and `FIBRE_HOST` changes take effect when the validator container is recreated.

For direct image use, mount `/data` on the validator and `/data/bridge` on the bridge. Set the bridge's `CORE_HOST` to the validator's hostname and use the same `P2P_NETWORK` in both containers. The images manage initialization, readiness, and startup failures themselves. `STARTUP_TIMEOUT` defaults to 120 seconds per validator startup phase and 120 seconds for bridge startup.

## Accounts and state

Both images contain `node-0` through `node-9` and `validator-0` credentials at `/credentials`: `.key` (armored; password `password`), `.plaintext-key` (hex), and `.addr`. The validator also has a `validator-0.valaddr` file in both images. The bridge uses `node-0`.

Each account receives `1000000000000000utia` in genesis and `1000000000000utia` in Fibre escrow, backed by the Fibre module account. Validator self-delegation and transaction fees reduce its spendable balance.

```sh
docker compose -f compose.yaml cp validator:/credentials ./credentials
docker compose -f compose.yaml restart
docker compose -f compose.yaml down -v  # Reset chain, Fibre, and bridge state.
```

Restart preserves blocks and blobs. Reset generates a fresh chain with the same account identities. Reset before incompatible version changes. The existing `local_devnet` example remains available separately.
