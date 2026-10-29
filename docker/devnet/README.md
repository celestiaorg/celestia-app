# Celestia devnet

One container runs a single validator with Fibre. The image contains fixed public test keys. Never use these keys for real funds.

## Published image

Download `compose.yaml`, set `DEVNET_VERSION` to an app release tag that includes the devnet image, and run:

```sh
docker compose -f compose.yaml up -d --wait
docker compose -f compose.yaml logs -f
```

The image is `ghcr.io/celestiaorg/celestia-devnet-validator:<app-tag>`.

The `devnet` workflow tests the image on amd64 and arm64, then publishes it for app release tags. To rerun publication, open **Actions → devnet → Run workflow** and set `tag` to the existing app release tag containing the devnet implementation.

## Build from source

From the repository root:

```sh
export DEVNET_VERSION=local
docker compose -f docker/devnet/compose.yaml -f docker/devnet/compose.build.yaml build
docker compose -f docker/devnet/compose.yaml up -d --wait
bash docker/devnet/smoke.sh
```

The smoke suite uses an isolated project name and dynamically allocated loopback ports. It removes its own volumes and saves logs under `/tmp/celestia-devnet-test-<pid>`; set `DEVNET_LOG_DIR` to choose another directory.

## Connections and settings

| Service | Host endpoint |
| ------- | ------------- |
| Validator RPC | `http://localhost:26657` |
| Validator REST | `http://localhost:1317` |
| Validator gRPC | `localhost:9090` |
| Fibre | `localhost:7980` |

Published ports bind to loopback. Validator signing stays inside the validator container.

| Environment variable | Default | Purpose |
| -------------------- | ------- | ------- |
| `P2P_NETWORK` | `devnet` | Network and chain ID; reset volumes when changing it |
| `BLOCK_TIME` | `1s` | Delayed precommit timeout |
| `FIBRE_HOST` | `localhost:7980` | Advertised Fibre address |

Container clients on the Compose network use `validator:26657` and `validator:9090`. Set `FIBRE_HOST=validator:7980` for these clients; host clients need an advertised address reachable from the host instead. `BLOCK_TIME` and `FIBRE_HOST` changes take effect when the validator container is recreated.

For direct image use, mount `/data`. The image manages initialization, readiness, and startup failures itself. `STARTUP_TIMEOUT` defaults to 120 seconds per startup phase.

## Accounts and state

The image contains `node-0` through `node-9` and `validator-0` credentials at `/credentials`: `.key` (armored; password `password`), `.plaintext-key` (hex), and `.addr`, plus `validator-0.valaddr`.

Each account receives `1000000000000000utia` in genesis and `1000000000000utia` in Fibre escrow, backed by the Fibre module account. Validator self-delegation and transaction fees reduce its spendable balance.

```sh
docker compose -f compose.yaml cp validator:/credentials ./credentials
docker compose -f compose.yaml restart
docker compose -f compose.yaml down -v  # Reset chain and Fibre state.
```

Restart preserves blocks and blobs. Reset generates a fresh chain with the same account identities. Reset before incompatible version changes. The existing `local_devnet` example remains available separately.
