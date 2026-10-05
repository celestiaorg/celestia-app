# Corto-10 bootstrap toolkit

Reproducibility material for the Corto-10 experimental network: a 10-validator,
20-encoder fibre throughput testnet on arm64 (`c8gn.12xlarge`, eu-central-1),
chain-id `corto-10`, app version 11.

This directory is a record of how that network was stood up. It is not a general
celestia-app deployment guide and none of these values are recommended defaults.

## Files

- `generate.py` — two-stage genesis generator (`prepare` then `finalize`).
- `bootstrap-transactions.py` — post-genesis provider registration and escrow deposits.
- `corto-10-config-baselines-20260929/` — `v10.3.0-corto` release defaults, the
  Corto-10 candidate defaults, and the parameter diffs between them. See that
  directory's `README.md`.
- `accounts.example.json` — redacted schema of the account manifest `generate.py`
  emits and the other tools consume. Real addresses come from the private keyrings
  and are never committed.
- `genesis.json` / `GENESIS.sha256` — the canonical live genesis. See `GENESIS.md`.
- `BUILD.md` / `build.sh` — how the deployed arm64 binaries were built and shipped.
- `deploy/` — templated service units, config deltas and host tuning.

## Steps

1. **Provision** — 10 validator + 20 encoder arm64 hosts (CloudFormation, in
   `celestiaorg/infrastructure`). Not reproduced here.
2. **Prepare** — generate all keys and a funded, not-yet-final genesis:

   ```
   ./generate.py prepare --binary ./celestia-appd \
     --private-dir /secure/corto-10-private --public-dir ./out
   ```

   Private keyrings are written under `--private-dir`, which must be outside the
   repository and mode `0700`. The public manifest and funded draft go to
   `--public-dir`.
3. **Finalize** — add gentxs with the real validator IPs and validate:

   ```
   ./generate.py finalize --binary ./celestia-appd \
     --private-dir /secure/corto-10-private --public-dir ./out \
     --inventory ./inventory.json
   ```

   `inventory.json` maps `validator-0`..`validator-9` to their host IPs, e.g.
   `{"validator-0": "10.110.0.100", ...}`. This produces `out/genesis.json`.
4. **Install** — ship the binaries and the final genesis to every host (`BUILD.md`).
5. **Start** — enable the service units (`deploy/`).
6. **Register / deposit** — once the chain is producing blocks, run
   `bootstrap-transactions.py register` once per validator (`valaddr set-host`)
   and `bootstrap-transactions.py deposit` once per encoder (128 keys each fund
   `500000000000utia` into fibre escrow).

## Genesis deltas vs `v10.3.0-corto`

| path | release default | Corto-10 |
|---|---|---|
| `staking.params.max_validators` | 100 | 10 |
| `fibre.params.full_stake_storage_budget` | 2 TiB (2199023255552) | ~1.5 PiB (1688849860263936) |
| `fibre.params.shard_retention` | 14400s (4h) | 86400s (24h) |
| consensus `version.app` | 10 | 11 |
| evidence `max_age_num_blocks` / `max_age_duration` | appconsts 404400 / 337h | stock CometBFT 100000 / 48h |

The evidence window (48h) is below the 14-day unbonding period; it is an
intentional choice for a bounded testnet.

## Account layout

Supply is 150,000,000,000 TIA across 1,291 accounts:

- 10 validator operators — 100,100 TIA each, 100,000 self-bonded at genesis.
- 1,280 encoder signing keys — `fibre-0`..`fibre-127` per encoder (10 encoders),
  1,000,000 TIA each; the first 32 per encoder are active (320 total).
- 1 treasury — 148,718,999,000 TIA.
