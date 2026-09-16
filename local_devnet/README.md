# Local v10 devnet

Two equal-stake celestia-app validators, two Fibre servers, and a funded test
account. Everything builds from this checkout. Fibre is included in v10 by
default. Chain ID: `local-devnet`.

Validators use `--force-no-bbr` so Docker Desktop's default kernel works.

## Run

Install Docker with Compose v2, Bash, and curl. Give Docker at least 8 GB RAM.
No host Go installation is needed. The first build downloads dependencies and
can take several minutes.

From the repository root:

```sh
cd local_devnet
./up.sh
```

This initializes genesis, starts both validators and Fibre servers, registers
`fibre-1:7980` and `fibre-2:7981` on-chain, and deposits test funds into Fibre
escrow. It waits for readiness and successful setup transactions before returning.
Subsequent runs reuse the existing keys and data.

Run the complete e2e flow:

```sh
./e2e.sh
```

It starts the network, checks v10, both validators, host registration, funding,
block agreement/progress, and endpoints. It submits four PFBs and four PFFs at two
sizes and downloads every PFF blob to check its contents. A failure exits nonzero;
a successful run leaves the network running. It also works on an existing devnet.

## Submit blobs

```sh
./submit-pfbs.sh --count 10 --size 1024 --interval 500ms
./submit-pffs.sh --count 5 --size 262144 --interval 1s --verify
```

Each transaction contains one random blob in namespace `localdev`. Both scripts
print committed transaction hashes and heights and stop on the first failure.
Run them sequentially: they share the same signing account.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--count` | `1` | Number of transactions |
| `--size` | `1024` | Bytes per blob, from 1 to 2097152 |
| `--interval` | `0s` | Pause after each committed transaction |
| `--timeout` | `1m` | Per-transaction timeout, including download |
| `--verify` | `false` | PFF only: download and compare the blob |

## Endpoints

Published ports bind to `127.0.0.1`.

| Service | Validator 1 | Validator 2 |
| --- | --- | --- |
| RPC | <http://localhost:26657> | <http://localhost:26658> |
| REST API | <http://localhost:1317> | <http://localhost:1318> |
| App gRPC (plaintext) | `localhost:9090` | `localhost:9091` |
| Fibre (TLS) | `localhost:7980` | `localhost:7981` |

```sh
curl -s http://localhost:26657/status
curl -s http://localhost:1317/valaddr/v1/all-bonded-fibre-providers
```

The supplied scripts run inside Docker, where registered Fibre names resolve.
For a Fibre client running on your host, add `127.0.0.1 fibre-1 fibre-2` to
`/etc/hosts`; use `localhost:9090` for app state. External test containers can
join `celestia-local-devnet_default` and use `validator-1:9090` directly.
The private validator signing ports are only accessible inside Docker.

## Test account

The `test` key uses the `test` keyring backend, at `/data/test` inside containers.
Genesis funds it with `1000000000000000utia`; setup moves `1000000000000utia` into
Fibre escrow. These are disposable local keys: never send real funds to them.

Read the address and recovery mnemonic for use in your own tests:

```sh
docker compose exec validator-1 cat /data/test-account.json
```

Query its balance (replace the address):

```sh
curl -s http://localhost:1317/cosmos/bank/v1beta1/balances/celestia1...
```

## Stop, restart, reset

```sh
docker compose logs -f validator-1 fibre-1 setup
docker compose down       # stop; preserve keys, chain, and Fibre data
./up.sh                   # resume
# Delete this devnet's keys and data, then create a fresh chain:
docker compose down -v
./e2e.sh
```

Both validators must run for blocks to progress. If startup fails, inspect
`docker compose ps -a` and `docker compose logs --tail 100`. An `init` or `setup`
container exiting with code 0 is normal. Port conflicts require stopping the
other local service or changing the published port in `compose.yaml`.
After changing app code, rebuild with `./up.sh`; reset the volume if the change
requires a new genesis or incompatible state.
