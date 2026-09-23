# Colocated Graviton throughput profile

Reference settings for 120 equal-stake validators, each running Fibre and a Go uploader. Size these settings against the target host's RAM and measure before sustained load. This example is documentation, not an automatically loaded Talis profile.

## Chain and accounts

Use a unique chain ID of at most 20 characters, for example `g120-0923-1435`. Configure 120 equal-stake validators. Provision 64 funded uploader accounts per validator, named `fibre-0` through `fibre-63`; the first 24 are active in this profile. Deposit 30,000,000 TIA into each active uploader escrow and 100,000 TIA into each of the remaining 40 escrows before load. Keep bank funds available for transaction fees.

## Fibre server

Merge these fields into each existing `fibre/config/server_config.toml`:

```toml
app_grpc_address = "127.0.0.1:9091"
signer_grpc_address = "127.0.0.1:26669"
server_listen_address = "<local-secondary-ip>:7980"
upload_verify_workers = 100
max_connections = 256
max_concurrent_streams = 200
unlimited_budget = false
storage_backend = "object"

[object_storage]
endpoint = "https://s3.<region>.amazonaws.com"
region = "<region>"
bucket = "<existing-legacy-bucket>"
hash_first_bucket = "<existing-hash-first-bucket>"
hash_first_bucket_next = "<existing-next-bucket>"
promise_hash_keys = true
packed_bucket = "<assigned-packed-bucket>"
prefix = "<unique-experiment>/validator-000"
request_timeout = 30000000000
batch_size = 16
```

Use the instance IAM role through the default AWS credential chain. Assign four operator-selected packed buckets round-robin by validator index modulo four. Keep existing destination fields and namespaces unchanged when objects already exist; older markers still route reads and pruning to their original destinations.

The packed implementation uses full batches of 16 without timer flushing, a 128 GiB packed-upload admission budget, and an aggregate 1,000-connection S3 transport limit per process. These are implementation settings, not additional TOML keys. Packed keys use eight groups per validator: the first promise-hash byte modulo eight selects the leading `00`–`07` group, followed by the existing validator namespace. Pruning runs every 24 hours, with no immediate startup pass. Retain compatible binaries and all generation configurations after newer markers are written.

## Uploader

Use the funded accounts above and submit through each node's own app endpoint:

```sh
fibre-txsim \
  --grpc-endpoint 127.0.0.1:9091 \
  --keyring-dir '<app-home>' --key-prefix fibre \
  --network-config '<per-host-network.json>' \
  --preencode --blob-size 2147483643 \
  --concurrency 24 --rpc-timeout 60s \
  --interval 0 --duration 10m \
  --otel-endpoint 'http://<collector>:4318'
```

Leave PFF submission enabled. Failed attempts use exponential backoff with jitter, capped at ten seconds and reset on success. The raw payload is 2,147,483,643 bytes; the encoded blob includes the five-byte header.

## Network and memory

Bind Fibre to the secondary NIC. Each uploader's network JSON contains only its secondary source IP and all validators' secondary destinations:

```json
{
  "source_ips": ["<local-secondary-ip>"],
  "validators": {
    "<UPPERCASE-CONSENSUS-ADDRESS>": ["<validator-secondary-ip>:7980"]
  }
}
```

Include all 120 validators and verify they are bonded before measuring the full validator set. Verify source-policy routing for both request and response traffic and authenticated secondary Fibre access. Keep S3 on the primary NIC and local app/signer endpoints unchanged. Use mq with fq leaves on both interfaces. Confirm actual socket paths and interface counters; two addresses alone do not prove two network cards.

For hosts with approximately 371 GiB usable RAM, use these systemd limits as a measured starting profile:

| Service | GOMEMLIMIT | MemoryHigh | MemoryMax |
| --- | --- | --- | --- |
| App | 16GiB | 20G | 24G |
| Fibre | 248GiB | 272G | 288G |
| Uploader | 24GiB | 28G | 32G |

Keep persistent app state, signing state and Fibre metadata on the mounted data volume. Do not restore stale signing state or metadata during rollback. Configure Fibre metric exports every five seconds with `OTEL_METRIC_EXPORT_INTERVAL=5000`.

## App fees and timing

Set the node-local filter explicitly in each app `config/app.toml`:

```toml
minimum-gas-prices = "0.000001utia"
```

The flat PFF transaction fee is 1 utia with a 1,000,000 gas limit. A stale `0.004utia` local filter rejects it with code 13; changing this filter does not change on-chain parameters.

The benchmark app launch profile uses `--delayed-precommit-timeout=1s --timeout-commit=500ms`, targeting a nominal 1.5-second cadence. These are runtime flags; observed block intervals also depend on consensus progress. Keep the 2,000-PFF block cap unless a separate coordinated change is approved.

## Ramp and validation

Start with 20 producers, then 40, 60, 80, 100 and 120. Hold each stage for 14 seconds and the final stage for 180 seconds; keep earlier producers running. Starting with fewer producers can starve full batches spread across eight groups. Monitor memory, chain progress, successful quorum uploads, PFF-confirmed bytes and S3 errors separately. Hold or stop when errors increase; queued bytes are not completed throughput.

Before the run, validate packed PUT/range-GET/reopen/pruning and legacy routing using an isolated namespace. Keep uploaders stopped while changing binaries, destinations or network paths. Stop and remask uploaders at the end of the controlled window.
