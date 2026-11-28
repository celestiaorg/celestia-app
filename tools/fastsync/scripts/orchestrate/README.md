# Multi-VM "fully verified" mode

`../fastsync.sh --checkpoints` saves node homes at checkpoint heights on one machine, and `../segment.sh`
re-executes one segment from such a home. These scripts spread that work over several VMs, with a bucket
between them:

```text
             bundle (verified with cmd/manifest)
                      |
  apply VM:   apply-checkpoints.sh   -- applier, every height checked against expected.tsv (fshdr)
                      |  every N blocks: application.db tarball + sha256 + CHECKPOINT (height, app hash)
                      v
          gs://BUCKET/prefix/checkpoints/<c>/
                      |
  segment VMs: segment-run.sh  (one segment per run; plan-segments.sh assigns them)
               download c, check sha256 + app hash == expected.tsv row c, fsboot bootstrap at c,
               then ../segment.sh: celestia-appd start --halt-height END+1, clean halt, boundary == expected.tsv
                      |
                      v
          gs://BUCKET/prefix/results/<c+1>-<END>/RESULT   (status=PASS|FAIL, reason, timings)
```

The range is verified when every segment's `RESULT` says `status=PASS`. Segment k ends where segment k+1
starts, and each checkpoint's app hash is pinned to the verified header chain twice: by the applier on the
apply VM (`-require-trusted`) and again on the segment VM.

## Scripts

| Script | Runs on | Does |
|---|---|---|
| `plan-segments.sh` | anywhere | Prints the segment plan (a table: segment, start checkpoint, heights, VM, wave) for a height range and VM count. |
| `apply-checkpoints.sh` | apply VM | Makes the bundle read-only, verifies it (`manifest verify`), applies up to each multiple of `--every` with the apply loop of `fastsync.sh` (`../lib-apply.sh`), then publishes the checkpoint: the tarball, then `CHECKPOINT` (chain id, height, app hash, tarball sha256, store format), which marks it complete. A rerun skips published checkpoints and resumes from `<work>/applied-to`. An app hash mismatch marks the DB `POISONED`. |
| `segment-run.sh` | segment VM | Builds the node home from a checkpoint and runs `../segment.sh`. Uploads `RESULT` and the last 200 log lines. Exit status: 0 PASS, 2 FAIL, 1 setup error. |
| `lib.sh` | (sourced) | Bucket layout, gcloud wrapper, TOML setter, on top of `../lib-apply.sh` (logging, dry run, apply loop). |

Tests: `go test ./scripts/...` (`../orchestrate_test.go`) runs the scripts with stub tools and the real `segment.sh`.

`apply-checkpoints.sh` does not wrap `fastsync.sh --checkpoints`, which keeps the homes on local disk and
returns only after the last height. Publishing each checkpoint as soon as it is applied lets segment VMs start
while the apply is still running. Both scripts share the apply loop.

Every script takes `--dry-run`, which prints each command that would change something instead of running it.
Bucket, chain id and paths are flags; tools come from environment variables (`APPLIER`, `APPLIER50`,
`MANIFEST_BIN`, `APPD`, `FSBOOT`, `FSHDR`, `FASTINDEX46`, `FASTINDEX50`, `SEGMENT_SH`, `GCLOUD`).

## Trust

- `expected.tsv` (fshdr output verified for the chain) is the only trust anchor on both VMs. The bucket is
  untrusted.
- On the apply VM, every applied height is checked against it, and the bundle is made read-only before
  `manifest verify` so it cannot change between the check and the apply (except by its owner or root).
- On a segment VM, the checkpoint's sha256 and `CHECKPOINT` fields are transport and consistency checks only.
  What binds the downloaded `application.db` to the verified chain is `fsboot`: it writes the state at c with
  the AppHash of the verified header c+1, and the CometBFT handshake at start refuses an app DB with a
  different app hash.
- The tarball is decompressed with a size cap (`--max-db-bytes`, default 1 TiB). Only `application.db` is
  extracted, without owners or modes from the archive, and any entry other than a regular file or directory
  fails the segment.
- Run both scripts as a dedicated non-root user.

## Speed settings on segment VMs

`segment-run.sh` writes these to the node home before the run:

- `--inter-block-cache sync-only` (default) sets `[performance] inter-block-cache` in app.toml, the
  multiplexer's sync-only inter-block cache for historical children. A node in this mode serves no queries,
  which suits a segment VM. Binaries without the setting ignore the key. `keep` leaves app.toml unchanged.
- `--max-requesters N` sets `[blocksync] max_requesters` in config.toml (celestia-core; default 40).
- `--fastindex` builds the IAVL fast-node index offline (`fastindex`, or `fastindex46` for checkpoints written by
  the v0.46 applier, from `store_format` in `CHECKPOINT`). Without it, the node rebuilds the index at its first
  start.
- WAL fsync during catch-up is already handled by celestia-core's default `wal_fsync = "catchup"`.

## Example (mainnet, 16 segment VMs)

```bash
P=gs://my-sync-bucket/run-2026-11
O=tools/fastsync/scripts/orchestrate
$O/plan-segments.sh --to 16000000 --every 250000 --vms 16
#   # plan: heights 1..16000000, checkpoint every 250000, 16 VM(s) -> 64 segment(s), 4 wave(s)

# apply VM: one long job; checkpoints appear as the apply passes them
APPLIER=./cstool46 APPLIER50=./cstool50 $O/apply-checkpoints.sh --home /data/apply --changesets /data/bundle \
  --expected /data/expected.tsv --chain-id celestia --bucket "$P" --every 250000 --to 15999999 \
  --manifest /data/manifest.json --pub /data/bundle.pub

# segment VM, for each of its rows of the plan (here segment 250001..500000)
$O/segment-run.sh --bucket "$P" --checkpoint 250000 --end 500000 --home /data/seg-250000 \
  --expected /data/expected.tsv --chain-id celestia --rpc https://rpc-a.example,https://rpc-b.example \
  --peer <node-id>@10.0.0.2:26656 --max-requesters 200 --fastindex

# collect
gcloud storage cat "$P/results/*/RESULT" | grep -c '^status=PASS'   # want 64
```

Notes:

- `--home` must be a **fresh** initialized home: config, genesis, app.toml and, for a multiplexer build, the
  embedded children under `<home>/bin`. Use a new one per segment.
- A segment can start once its `CHECKPOINT` object exists (`gcloud storage ls $P/checkpoints/<c>/CHECKPOINT`).
  The genesis segment can start right away. A segment must span at least 20 blocks, because `segment.sh`
  checks 20 boundary rows.
- `--peer` serves blocks c+1..END+1. `fsboot` needs two distinct `--rpc` servers (or `--allow-single-source`,
  or `--boot-src-data`) for the validator sets and commit at c, and checks them against the verified header hash.
- Checkpoint tarballs hold the whole app DB, hundreds of GB late in the chain. The apply VM needs scratch disk
  for one tarball, and the bucket needs room for all of them until the segments finish. Delete
  `$P/checkpoints/` afterwards.
- No script here creates VMs. One way to drive them without SSH is a GCS command channel: an agent on each
  VM runs new `gs://CTL/cmd<N>/*.sh` scripts under `systemd-run` and uploads their output. Use no runtime limit
  for multi-hour segments, and `KillMode=mixed` so multiplexer children stop with the parent. A driver drops
  one `segment-run.sh` command per plan row into `cmd<vm>/` and polls `results/`.

## Tools used

- the applier interface of `fastsync.sh` (see `../lib-apply.sh`)
- `fsboot bootstrap`
- `../segment.sh`, which runs `celestia-appd` and `fshdr headers`
- `fastindex` / `fastindex46`
- `manifest verify -print-env`
- `gcloud`, `tar`, `zstd`, `sha256sum`, `awk`
