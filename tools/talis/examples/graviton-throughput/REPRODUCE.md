# Reproducing the Graviton throughput benchmark

## Scope and pinned artifacts

Recreate a dedicated benchmark network. Use a fresh chain ID, for example `graviton-bench-120`, with 120 equal-stake validators, each colocating an app, Fibre server and tx-sim. The source branch is [integrate/fibrrrrrr-preston-graviton](https://github.com/celestiaorg/celestia-app/tree/integrate/fibrrrrrr-preston-graviton). Final deployed source was `83f2e81c5ab42c97039e0adf2d557e169ab90396`; `f7ea0991` added documentation rather than replacing the deployed binaries. Resolve/pin full commit IDs from the retained source manifest or Git before rebuilding.

The fleet-verified SHA-256 values in `fresh120/stage/manifest.json` are:

| Binary | SHA-256 |
|---|---|
| celestia-appd | `7d9bcf90607aee972fb21adc628cb77b95a7758fcccf98b67ea2b83ca76cb283` |
| fibre | `f659fb86b9c1fc195f3df01b4349d8f2eeb99e147fc30fd85c68f00f7135797f` |
| fibre-txsim | `354fef6356cac1f67d839f844512cd4f30cc3871ab1f5984ac7de7cadc066d12` |

Genesis SHA-256: `f8d0ed7d877912a9efe5178fc93e75f1cdab2bb86aa2b6e4d01df7fdd75ed3d4`. Retain `fresh120-short/genesis.json`, `manifest.json`, `diff-audit.json`, and `deployment-manifest.json`. The short-chain correction changed only chain ID and signed gentxs relative to its preceding 120-node genesis. The initial expansion preserved module parameters except the explicitly increased maximum validator count.

**Benchmark exception:** the branch disables PFF validation more broadly than just a cryptographic micro-optimization and removes the row-floor check (`ba60f5497db3621086ec46d53e0407c80044156f` and `5130ec6013d7d0bca5f5538915e3778c5d01a400`). This behavior is part of this result and must be disclosed. Do not treat this as a production deployment recipe. Ordinary consensus signing and TLS were retained.

## Infrastructure

| Item | Final setup |
|---|---|
| Validators | 120 AWS c8gn.48xlarge, ARM64, 192 logical CPUs, approximately 371 GiB usable RAM each |
| Placement | eu-west-1, availability zone eu-west-1c |
| Networking | Two network cards/interfaces per host, nominal 300 Gbps each / 600 Gbps aggregate |
| Storage | Root volume plus a separate 100 GiB EBS data volume, XFS; app state/signing state and Fibre metadata on the data mount |
| S3 | AWS S3 eu-west-1, `https://s3.eu-west-1.amazonaws.com`, instance IAM role/default credential chain |
| S3 route | Operator-confirmed VPC S3 gateway endpoint; primary NIC default route |
| Monitoring | Separate host, Prometheus, Grafana, OTLP collector; port 4318 from validators |

Private inventory and disk/NIC discovery are retained separately. Verify actual NIC-to-network-card mapping, mount UUIDs, disk performance provisioning, security groups and routing on replacement infrastructure; two IPs alone do not prove independent cards. No exact EBS IOPS/throughput entitlement is inferred from its size.

## Network paths and Linux setup

- App P2P: private TCP 26656; RPC 26657; each tx-sim uses its own app gRPC `127.0.0.1:9091`; Fibre signer `127.0.0.1:26669`.
- Fibre listens only on that host's secondary private address, TCP 7980. Each client uses a JSON consensus-address-to-secondary-endpoint map covering all 120 validators. This is a runtime configuration override, not a block-header address and not a binary-hardcoded address.
- `source_ips` contains the local secondary private IP. The client binds its socket source; Linux source policy routing table 101 sends that source through the secondary interface. Server responses follow the matching source policy route.
- S3 stays primary-only. The proposed dual-NIC S3 transport was canceled and is not part of this result.
- BBR was enabled. Primary multiqueue leaf schedulers were changed to `fq`, preserving hardware multiqueue; secondary used `fq`. Preserve the captured exact `tc`, `ip rule`, `ip route`, sysctl and ethtool snapshots instead of indiscriminately replacing the root qdisc.
- Validate actual connected source/destination tuples, all-peer reachability, authenticated Fibre requests, and per-interface byte/ENA counters. Historical 100-node verification tested 10,000 paths; use a fresh 120-node matrix for reproduction.

Network JSON shape:

```json
{"source_ips":["<local-secondary-ip>"],"validators":{"<UPPERCASE-CONSENSUS-ADDRESS>":["<peer-secondary-ip>:7980"]}}
```

## Fibre storage and limits

| Setting | Final value |
|---|---|
| Verification workers | 100 per Fibre process |
| Incoming connections / concurrent streams | 256 / 200 |
| Object batch size | 16 shards, full batches; no timer-based partial flush |
| Packed upload admission budget | 128 GiB per server |
| Packed object maximum | 4 GiB |
| Object request timeout | 30 seconds including retries |
| S3 transport connection cap | 1,000 per process |
| S3 batch buckets | `lungo-test-0`, `lungo-test-3`, `lungo-test-5`, `lungo-test-7` |
| Assignment | bucket list[index % 4], 30 validators per bucket |
| Key groups | 8 per validator; leading group from first promise hash byte modulo 8, then validator namespace |
| Prune loop | Every 24 hours; no immediate startup prune |
| Upload coordination | Exact-promise identity coordinator (`69bf8e8fec43dd1d7d124e919fa5f399e38adf2d`); historical 2,048 striped upload locks were replaced |

Use a fresh run namespace, retain exact object generation configuration and metadata. Legacy bucket fields (`bucket`, `hash_first_bucket`, `hash_first_bucket_next`) and newer packed namespace markers route retained objects to their original bucket/key layouts. Never repoint old generations or roll back to a binary that cannot read a generation already written. Packed objects require their per-shard metadata for range reads and deletion; an S3 bucket alone is not the complete restore artifact.

Full batches can stall when too few producers populate eight groups; begin at 20 producers. The historical 30 TiB per-validator quota intent was expressed through chain storage-budget parameters; the exact final `full_stake_storage_budget` is recorded below and must take precedence over a rounded description.

The upload path avoids S3 HEAD before object writes; conditional PUT semantics and normal existence checks retain their distinct roles. Physical PUT metrics, logical successful shard counters, client quorum bytes and confirmed PFF bytes are different quantities. Conditional duplicate acceptance can inflate logical byte counters.

## Process resources and tx-sim

| Service | GOMEMLIMIT | MemoryHigh | MemoryMax |
|---|---|---|---|
| App | 16GiB | 20G | 24G |
| Fibre | 248GiB | 272G | 288G |
| Tx-sim | 24GiB | 28G | 32G |

These are configured limits, not assertions of final-run peak usage. Use captured systemd units/drop-ins as the exact source, including restart behavior, mount dependencies, file limits and environment. Fibre exports metrics every 5 seconds.

```sh
fibre-txsim \
  --grpc-endpoint 127.0.0.1:9091 \
  --keyring-dir '<app-home>' --key-prefix fibre \
  --network-config '<network.json>' \
  --preencode --blob-size 2147483643 \
  --concurrency 24 --rpc-timeout 60s \
  --interval 0 --duration 10m \
  --otel-endpoint 'http://<collector>:4318'
```

There are 24 workers per host, 2,880 total. Always use preencoding for this comparison: 2,147,483,643 raw bytes plus a five-byte blob header. Preencoded payload/commitment reuse is deliberate; this does not measure fresh encoding at the reported rate. Failed upload attempts back off with jitter from approximately 250–500 ms, doubling to a 5–10 second range and resetting on success. PFF submission remains enabled.

Provision 64 local uploader keys per host (7,680 total); the active first 24 have 30 million TIA escrow each, remaining 40 have 100,000 TIA each. Genesis escrow total is 86,880,000,000 TIA. Preserve exact bank balances and validator allocations from genesis; total supply is 666,000,000,000 TIA, reserve 574,679,877,120 TIA. Key names must match `fibre-0` through `fibre-63`; the additional hosts initially used `fibre0` and required SDK key rename without changing addresses.

Set local app `minimum-gas-prices = "0.000001utia"` as well as the matching onchain minimum. The final fee-fixed run depended on this: a correct consensus fee parameter does not override a stricter local CheckTx policy. PFF flat fee was 1 utia, not 1 TIA. Both final consensus and soft PFF-per-block caps were 2,000.

## Deployment sequence

1. Preserve the source commit, manifests, genesis, script versions and configuration snapshots before provisioning. Generate fresh keys locally on their intended hosts; never copy an old signing state into a new running signer.
2. Provision both network cards and the data volume; configure stable mounts and source routing, private security-group paths, monitoring reachability and S3 IAM permissions. Check correct filesystem before any formatting. Keep secrets out of artifacts.
3. Build the pinned ARM64 source using its documented build targets/toolchain, or reuse the hash-verified artifacts. The Go module replacement points to `./third_party/reedsolomon`; do not accidentally substitute upstream codec code. Run `make build` for source changes. Preserve `go version -m` output for all binaries.
4. For distribution, upload each public binary/config artifact once to a deployment S3 location and download in parallel, checking SHA-256 on every node. Do not put keys/keyrings in this distribution bundle.
5. Build genesis with exact preserved module/consensus parameters, equal validator stake, 120 validators and all funded accounts. Validate with the pinned app binary. Chain ID must be at most 20 bytes; retain signed gentxs for that exact ID.
6. Install each app with matching genesis, peer configuration, disk mount, local minimum fee and captured consensus timeout overrides. Start apps, verify all 120 catching-up flags false, common height/hash and validator power. Use app flags `--delayed-precommit-timeout=1s --timeout-commit=500ms --pff-proposal-limit=2000`, with `app-db-backend = "goleveldb"`. Actual block latency must be measured, not assumed from these flags.
7. Register every Fibre provider with its intended secondary endpoint. Check indexed code-zero receipts and provider state. Start Fibre only with correct chain identity, signer, local app and object namespace.
8. Verify all 64 account lookups, all three binary hashes, secondary routing/listener, four-bucket assignment and resource limits per node. Keep tx-sim gated until ready. A stale long chain ID or mismatched key names previously caused failed starts.
9. Use the retained `load/ramp_run.py` and its dependencies/configuration (portable copies in `evidence/final/reproduction/deployment-tools/`). Read that directory's README before adapting paths and inventory; it is not a turnkey deployment command. Start producer stages 20,40,60,80,100,120, minimum 14 seconds per stage, retain earlier producers, hold final service stage 180 seconds. The latest actual full-upload window was shorter because preencoding occurs after service startup.
10. Sample chain progress, physical S3 errors, client successful uploads, memory events and both NIC counters throughout. Stop on genuine health failure. At completion stop/remask tx-sim and remove its load gate, retaining app/Fibre and evidence until capture completes.

Scripts contain deployment-specific inventory and paths: adapt them after inspecting the saved profile, rather than blindly running an old restart/reset command. Fresh-chain reset was chosen for this benchmark only after preserving old data; normal app rollback was not used to repair consensus signing state.

## Measurement and archive

Report the full-load average alongside fixed-window maxima: **383.3 GB/s average**, **461.1 GB/s peak over 60 seconds**, and **534.2 GB/s peak over 30 seconds** in the final run. Use complete rolling windows contained in the actual all-producer interval, count `(start, end]`, and retain transaction-hash deduplication and indexed-height attribution. See [the calculation and per-block aggregates](throughput-windows.json). Do not report an unspecified instantaneous peak.

Use `load/final-run/ramp-summary-confirmed.json` and matching chain scan, actual-start and latency JSON. Count each transaction hash once, require its indexed receipt code zero, and attribute it to the latest successfully indexed inclusion height. Multiply by 2,147,483,643 raw bytes, divide by actual overlap seconds and 10^9 for decimal GB/s. Do not use physical block appearances as independent successes. A subsequent audit found zero cross-transaction-hash duplicate payment-promise identities among the captured successful transactions; payload contents were still deliberately reused.

The final result uses 25,597 unique indexed PFFs / 143.412226915 seconds = 383.294646434 GB/s. The same window contained 27,808 physical PFF appearances. Whole-run duplicate block appearances were 2,482. Historical block-results receipts were unavailable and all 357 measured raw blocks were already pruned when final archive collection began. All 42,204 indexed raw transactions and code-zero receipts were subsequently captured (approximately 249.6 MB compressed); retain their capture manifest/checksums. These and contemporary scans are the evidence, not a complete re-verifiable raw-block/finalization archive.

Retain: all run JSON and journals; config/systemd/network/kernel snapshots; genesis and provider state; all source/build manifests; source commit/bundle and ARM binaries; final metrics with query definitions and Grafana JSON; disk layout/volume settings; S3 routing/config/marker metadata; archive checksums and collection errors. Keep signing keys and IAM credentials outside the shareable archive. EC2, EBS, monitor and S3 objects/requests continue billing until independently torn down; stopping tx-sim does not delete resources.

## Exact genesis parameter snapshot

The following was extracted from the preserved final genesis; restore the complete genesis template rather than only selected fields.

```json
{
  "consensus_params": {
    "block": {
      "max_bytes": "33554432",
      "max_gas": "-1"
    },
    "evidence": {
      "max_age_num_blocks": "404400",
      "max_age_duration": "1213200000000000",
      "max_bytes": "1048576"
    },
    "validator": {
      "pub_key_types": [
        "ed25519"
      ]
    },
    "version": {
      "app": "11"
    },
    "abci": {
      "vote_extensions_enable_height": "0"
    }
  },
  "module_params": {
    "auth": {
      "max_memo_characters": "256",
      "tx_sig_limit": "7",
      "tx_size_cost_per_byte": "10",
      "sig_verify_cost_ed25519": "590",
      "sig_verify_cost_secp256k1": "1000"
    },
    "bank": {
      "send_enabled": [],
      "default_send_enabled": true
    },
    "blob": {
      "gas_per_blob_byte": 8,
      "gov_max_square_size": "256"
    },
    "distribution": {
      "community_tax": "0.020000000000000000",
      "base_proposer_reward": "0.000000000000000000",
      "bonus_proposer_reward": "0.000000000000000000",
      "withdraw_addr_enabled": true
    },
    "fibre": {
      "withdrawal_delay": "86400s",
      "payment_promise_timeout": "3600s",
      "payment_promise_height_window": "1000",
      "shard_retention": "600s",
      "full_stake_storage_budget": "912891816358885"
    },
    "gov": {
      "min_deposit": [
        {
          "denom": "utia",
          "amount": "10000000000"
        }
      ],
      "max_deposit_period": "604800s",
      "voting_period": "10s",
      "quorum": "0.334000000000000000",
      "threshold": "0.500000000000000000",
      "veto_threshold": "0.334000000000000000",
      "min_initial_deposit_ratio": "0.000000000000000000",
      "proposal_cancel_ratio": "0.500000000000000000",
      "proposal_cancel_dest": "",
      "expedited_voting_period": "5s",
      "expedited_threshold": "0.667000000000000000",
      "expedited_min_deposit": [
        {
          "denom": "utia",
          "amount": "50000000000"
        }
      ],
      "burn_vote_quorum": false,
      "burn_proposal_deposit_prevote": false,
      "burn_vote_veto": true,
      "min_deposit_ratio": "0.010000000000000000"
    },
    "minfee": {
      "network_min_gas_price": "0.000001000000000000"
    },
    "slashing": {
      "signed_blocks_window": "10000",
      "min_signed_per_window": "0.001000000000000000",
      "downtime_jail_duration": "60s",
      "slash_fraction_double_sign": "0.020000000000000000",
      "slash_fraction_downtime": "0.000000000000000000"
    },
    "staking": {
      "unbonding_time": "1213200s",
      "max_validators": 120,
      "max_entries": 7,
      "historical_entries": 10000,
      "bond_denom": "utia",
      "min_commission_rate": "0.050000000000000000"
    },
    "transfer": {
      "send_enabled": true,
      "receive_enabled": true
    },
    "warp": {}
  }
}
```
