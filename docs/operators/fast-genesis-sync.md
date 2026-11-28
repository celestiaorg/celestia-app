# Fast genesis sync

This guide is for operators who sync a node from genesis (or from a bootstrapped state) with the multiplexer build of `celestia-appd`. Syncing replays every app version era through the embedded binaries, so the settings below matter much more than they do at the tip.

> **Status: draft.** Measured speeds are marked `TODO` and will be filled in from measurements before this guide is final. The sync speedup options, the storage settings and the [hybrid path](#hybrid-path-verified-changesets) describe open pull requests and can change before release.

## Quick checklist

1. Edit the config files before the first start (see [Recommended config](#recommended-config)).
2. Run the node under systemd with `KillMode=mixed` and decide on `db_wal_fsync` (see [Run under systemd](#run-under-systemd) and [Storage settings while catching up](#storage-settings-while-catching-up)).
3. Keep block retention off until the node is past the v4 upgrade, or start from an empty blockstore (see [Block retention](#block-retention)).
4. Switch to the normal tip configuration once the node has caught up.

## Recommended config

Defaults for a freshly initialized node already set some of these, but an existing home directory keeps its old values, so check each one.

`config/config.toml`:

```toml
log_level = "warn"

[tx_index]
indexer = "null"
```

`config/app.toml`:

```toml
# No state sync snapshots while syncing. Snapshots are expensive and a node
# that is still catching up cannot usefully serve them.
[state-sync]
snapshot-interval = 0

# Skip building IAVL's fast-node index. It is rebuilt only if you turn it back on.
iavl-disable-fastnode = true
```

Notes:

- `log_level = "warn"` removes per-block logging, which is a measurable cost at tens of blocks per second.
- A `null` indexer means transaction and block event queries (`/tx_search`, `/block_search`) return nothing for the synced range. Re-enable the indexer and reindex if you need those queries.
- With `snapshot-interval = 0` the node cannot serve state sync snapshots until you restore a non-zero value. Remember to restore it after catching up (see [After the node has caught up](#after-the-node-has-caught-up)).
- `iavl-disable-fastnode = true` only affects the speed of some reads. It does not change the app hash.

## Run under systemd

With the default `KillMode=control-group`, `systemctl stop` sends SIGTERM to the parent and the embedded child at once. The child exits while the parent is mid-block, the parent's `FinalizeBlock` fails and it panics before closing its databases (see [celestia-app#8778](https://github.com/celestiaorg/celestia-app/issues/8778)). `KillMode=mixed` sends SIGTERM to the parent only; the parent stops its node and then the child, and systemd kills anything still alive after `TimeoutStopSec`.

```ini
[Unit]
Description=Celestia node (genesis sync)
After=network-online.target
Wants=network-online.target

[Service]
User=celestia
ExecStart=/usr/local/bin/celestia-appd start --home /home/celestia/.celestia-app
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

# Signal only the main process on stop; the parent stops the embedded child itself.
KillMode=mixed
# Give the parent time to stop CometBFT, stop the child and close its databases.
TimeoutStopSec=300

[Install]
WantedBy=multi-user.target
```

Use `Restart=on-failure` rather than `always` so that a deliberate stop or a `halt-height` is not restarted in a loop.

## Storage settings while catching up

These `config/config.toml` options are read by the parent's CometBFT, so they apply to every era. They change only how the node's databases are written, never what is computed. Both are pebbledb only.

### `[storage] db_wal_fsync`

```toml
[storage]
db_wal_fsync = "catchup"   # default; "always" restores stock behaviour
```

- `"catchup"` skips the fsync of the database write-ahead logs (WAL) only while block syncing. Every write still reaches the kernel before it returns, so a process crash (panic, SIGKILL, OOM kill, a `TimeoutStopSec` kill) loses nothing.
- A WAL that skipped a sync is fsynced when it is closed, and every open WAL is fsynced before the node switches to consensus. At the tip the node fsyncs as stock does.
- It does not touch the consensus WAL (`cs.wal`) or `priv_validator_state.json`.
- Before the first skipped fsync the node writes `data/db_wal_fsync_skipped`, holding the OS boot ID. After a process crash (same boot) the node fsyncs the WALs, removes the marker and starts.
- After a power loss or kernel crash during catch-up (different boot), the databases may be out of order with each other and the node refuses to start. Recover with one of:
  1. `rollback`, which also clears the marker, then restart;
  2. re-sync from a state sync snapshot, keeping `priv_validator_state.json` (never delete or reset it);
  3. start once with `--accept_unsynced_db_crash` to try the handshake anyway.

Set `db_wal_fsync = "always"` if you cannot accept that risk. Sources: [celestia-core#3819](https://github.com/celestiaorg/celestia-core/pull/3819) (backport [celestia-core#3822](https://github.com/celestiaorg/celestia-core/pull/3822)).

### Pebble tuning and shared cache

```toml
[storage]
pebble_tuning = true              # default
pebble_cache_size_mb = 0          # 0 = automatic
pebble_memory_reserve_mb = 20480  # default
```

With `pebble_tuning = true` every node database shares one block cache and gets bloom filters, larger blocks and files, and `clamp(CPUs / 4, 2, 8)` concurrent compactions. The on-disk format is unchanged; `false` restores the engine defaults (8 MiB cache per database). With `pebble_cache_size_mb = 0` the cache is sized from the machine:

```text
RAM   = min(MemTotal, smallest cgroup memory limit, v1 or v2)
cache = clamp((RAM - pebble_memory_reserve_mb) * 2/5, 256 MiB, 64 GiB)
```

For example 32 GiB gives 4.8 GiB, 64 GiB gives 17.6 GiB and 128 GiB gives 43 GiB. The reserve is left for the embedded app process, the node's heap and the OS; the app should stay within about the reserve minus 4 GiB. The node's worst case is about twice the cache plus 2.5 GiB, because the cache lives on the Go heap. Source: [celestia-core#3821](https://github.com/celestiaorg/celestia-core/pull/3821) (on `main` only).

## Block retention

The default `min-retain-blocks` is 9750 (`2 * SnapshotInterval`, see [`pkg/appconsts/initial_consts.go`](../../pkg/appconsts/initial_consts.go)). The first embedded child that returns a retain height is the v4 binary. At that point the parent prunes every block below the retain height synchronously inside block processing. After a long sync that is millions of blocks and the node stalls for hours (see [celestia-core#3793](https://github.com/celestiaorg/celestia-core/issues/3793)).

Pick one:

- **Retention off until past v4.** Set `min-retain-blocks = 0` in `config/app.toml` and restore it after the node has moved past the v4 upgrade height. Pruning then starts from a small backlog.
- **Start from an empty blockstore.** Bootstrap CometBFT at the height `H` of an application DB you already have (for example one built by the [hybrid path](#hybrid-path-verified-changesets)), instead of replaying history. The CometBFT state store and block store must be empty. Use either:
  - the stock command, `celestia-appd comet bootstrap-state --height H`. It verifies `H` with a light client configured in `config.toml` `[statesync]` (`rpc_servers`, `trust_height`, `trust_hash`, `trust_period`), so the trust root must be inside the trusting period;
  - `fsboot bootstrap` from `tools/fastsync` ([celestia-app#8808](https://github.com/celestiaorg/celestia-app/pull/8808), draft). It writes the same bytes as the stock command, checked against a verified header `H+1` from `fshdr`, so it needs no light client or trusting period. It requires at least two distinct `-rpc` servers that return identical values, unless you pass `-allow-single-source` or read from a node you run with `-src-data`.

  ```sh
  fsboot bootstrap -home ~/.celestia-app -height H -expected expected.tsv \
    -rpc https://rpc-a.example,https://rpc-b.example
  ```

  No header authenticates validator proposer priorities or the consensus params other than block max bytes and max gas. Both commands take them from their source, so a single source is trusted for them.

Note that the start hook only raises a non-zero `min-retain-blocks` to the minimum; `0` is left alone.

## Deferred persistence across upgrades

No restart is needed at any upgrade. Each binary that executes blocks decides at commit time whether to defer writes, and a DB left in deferred state by one binary is picked up by the next:

- The v3 child turns deferred persistence on at the first block of a sync from genesis.
- At the v4 upgrade the trees are still in the IAVL v0.19 format. The v4 child executes one block with per-block writes, then deletes the versions below the latest (log line `deferred persistence: deleted the versions below the latest to turn the mode on`) and turns the mode on.
- Every later binary loads the DB with the mode already on (`deferred IAVL persistence on`).
- At the tip the node logs `deferred IAVL persistence off: caught up` and writes every block.

If the mode stays off, the binary logs the reason once at `info` level (`deferred IAVL persistence stays off while catching up`). With `log_level = "warn"` that line is hidden; use `log_level = "info"` for the first minutes after an upgrade if you want to confirm the mode.

## Inter-block cache

The multiplexer starts the v4 to v8 children with the inter-block cache disabled because of races with concurrent queries ([celestia-app#7750](https://github.com/celestiaorg/celestia-app/pull/7750)): a concurrent read can leave a stale value in the cache and fork the node. A patched v4 to v8 child can turn the cache on in a mode that serves nothing else ([celestia-app#8809](https://github.com/celestiaorg/celestia-app/pull/8809), draft):

```toml
[performance]
inter-block-cache = "sync-only"    # default "off"
inter-block-cache-size = 100000    # entries per store, read only in "sync-only" mode
```

- While a v4 to v8 child runs in this mode it refuses CheckTx, ABCI queries (except peer filtering), every gRPC and REST request, Simulate and state sync. The node cannot accept transactions or answer queries until it is past v8. Block execution and `Info` are unchanged.
- Only patched v4 to v8 children read the key; v3, v9 and later, and unpatched children ignore it. The parent validates both keys at startup.
- `"off"` leaves the current behaviour unchanged. This key is distinct from the SDK's top-level `inter-block-cache` bool, which the multiplexer keeps forcing to `false` for v4 to v8.
- Results, events, gas and the app hash do not change. A prototype measured about 21% more blocks per second in the v6 era.

## Sync speedups

The sync speedups are on by default. The ones that carry risk have a config option to turn them off. None of them changes the app hash, events or gas; they only change how fast blocks are verified, executed and written. The option names below are taken from the open pull requests and can still change before release.

### Which eras get which speedup

The parent `celestia-appd` runs CometBFT for every height, so the `config.toml` options apply to every era. The `app.toml` options are read by the binary that executes the block: the embedded child for v1 to v11, the parent from v12. The embedded children share the parent's home directory and read the same `config/app.toml`; a child ignores keys it does not know. Pass these options in `app.toml`, not as command-line flags: the parent rejects flags it does not define, and it does not define the child-only ones.

Once every embedded child is patched, the eras get:

| App version era | Binary (cosmos-sdk line) | `app.toml` `[performance]` options |
| --- | --- | --- |
| v1 to v3 | v3 child (v0.46) | `signature-preverify`, `parallel-commit`, `execution-caches`, `deferred-persistence`, `deferred-flush-interval`; `delivertx-batch` (read by the parent) |
| v4, v5 | v4 and v5 children (v0.50) | `parallel-commit`, `async-pruning`, `execution-caches`, `deferred-persistence`, `deferred-flush-interval` |
| v6 | v6 child (v0.51) | `parallel-commit`, `async-pruning`, `execution-caches`, `deferred-persistence`, `deferred-flush-interval` |
| v7 to v11 | v7 to v11 children (v0.52) | `parallel-commit`, `async-pruning`, `execution-caches`, `deferred-persistence`, `deferred-flush-interval` |
| v12 and later | parent (main) | `parallel-commit`, `async-pruning`, `execution-caches`, `deferred-persistence`, `deferred-flush-interval` |

An era only gets a speedup if the child embedded in your release is patched. An unpatched child ignores these keys. Patched builds of every embedded child (v3.13.0, v4.1.0, v5.0.12, v6.4.10, v7.0.3, v8.0.8, v9.0.8, v10.5.0 and v11.0.0) exist on unmerged branches and pass the multi-version determinism test ([celestia-app#8792](https://github.com/celestiaorg/celestia-app/pull/8792)). Check the release notes of the release you run.

The patched children are on these celestia-app branches; the parent with every speedup is `claude/awesome-brahmagupta-sip7sa-parent-all`:

| Embedded version | Branch |
| --- | --- |
| v3.13.0 | `claude/awesome-brahmagupta-sip7sa-child-v3.13.0` |
| v4.1.0 | `claude/awesome-brahmagupta-sip7sa-child-v4.1.0` |
| v5.0.12 | `claude/awesome-brahmagupta-sip7sa-child-v5.0.12` |
| v6.4.10 | `claude/awesome-brahmagupta-sip7sa-child-v6.4.10` |
| v7.0.3-mocha | `claude/awesome-brahmagupta-sip7sa-child-v7.0.3-mocha` |
| v8.0.8 | `claude/awesome-brahmagupta-sip7sa-child-v8.0.8` |
| v9.0.8 | `claude/awesome-brahmagupta-sip7sa-child-v9.0.8` |
| v10.5.0 | `claude/awesome-brahmagupta-sip7sa-child-v10.5.0` |
| v11.0.0 | `claude/awesome-brahmagupta-sip7sa-child-v11.0.0` |

The multiplexer embeds release binaries by tag (`CELESTIA_V*_VERSION` in the `Makefile`). Before a release can embed a patched child, its pseudo-version dependencies must be replaced by tagged releases, the child must be tagged and released, and the `Makefile` must point to that tag. Until then a release embeds the unpatched children above.

### `config/config.toml`

| Option | Default | What it does | When to disable |
| --- | --- | --- | --- |
| `parallel_commit_verify` (top level) | `true` | Verifies the signatures of large commits on several cores. Results are identical to sequential verification. | If it causes CPU contention with other processes on the host. |
| `[blocksync] pipeline` | `true` | Prepares the next block (part set and commit verification) while the current one is applied. The synced blocks and state are identical. | If you suspect a blocksync problem; `false` prepares each block after the previous one is applied. |
| `[blocksync] max_requesters` | `40` | Number of blocks requested ahead of the one being applied, in `[2, 1000]`; `0` means the default. Worst-case memory is `max_requesters` times the maximum block size. | Lower it on hosts with little memory. Raising it helps only when peers are slow. A prototype measured a gain with `160` between heights 3.5M and 4.7M; whether the default changes is decided by the speed measurements. |

### `config/app.toml`

All keys are in the `[performance]` section. A missing key means its default, so an `app.toml` written by an older release gets the speedups without editing.

| Option | Default | What it does | When to disable |
| --- | --- | --- | --- |
| `signature-preverify` | `true` | Verifies a block's secp256k1 signatures in parallel before executing it and caches the valid ones. v1 to v3 only. | If it causes CPU contention. |
| `delivertx-batch` | `true` | The parent sends all transactions of a v1 to v3 block to the child in one call instead of one call per transaction. Used only when the child supports it; blocks over 64 MiB use per-transaction calls. Not written to a new `app.toml`; add the key to turn it off. | If the parent and the v3 child fail to communicate. |
| `parallel-commit` | `true` | Writes, hashes and commits the stores in parallel. | If you suspect a store write problem. |
| `async-pruning` | `true` | Prunes old IAVL versions in the background instead of inside `Commit`. v4 and later only. | If pruning falls behind or disk use grows. |
| `execution-caches` | `true` | Caches decoded params and validators (and, on v1 to v3, bech32 addresses) during block execution. Gas and results are unchanged because the store is still read. | If you suspect stale cached data. |
| `deferred-persistence` | `"sync"` | `"sync"` keeps new IAVL nodes in memory while the node is catching up and writes them every `deferred-flush-interval` blocks; at the tip it flushes and goes back to writing every block. `"off"` writes every block. See [Deferred persistence](#deferred-persistence). | If you need app-state rollback, or the host has little memory. |
| `deferred-flush-interval` | `100` | Blocks between flushes in deferred mode. After a crash the node replays at most this many blocks. | Lower it to bound memory and replay after a crash. |

`blocksync-events-off` is not part of the default set. It exists only on unmerged branches, defaults to `false` and changes what `/block_results` returns for synced blocks, so leave it unset.

### Deferred persistence

`"sync"` turns on only when both of these hold:

- The node is catching up: the block time is more than 10 minutes behind the wall clock. The wall clock only decides when data is written, never what is computed.
- The node keeps no history that deferred mode would delete. On v4 and later, with `pruning = "everything"`, the node deletes the few versions stock pruning still holds and turns the mode on; a larger history (for example right after switching from `"default"`) is left to stock pruning first, so the mode turns on once that has caught up. With any other pruning setting the mode turns on only when every tree holds at most one version below the latest. On v1 to v3, pruning must not keep more recent versions than `deferred-flush-interval`, state sync snapshots must be off (`snapshot-interval = 0`) and IAVL lazy loading must be off.

The default `pruning = "default"` keeps history, so with it deferred persistence stays off. For a fast sync from genesis set:

```toml
pruning = "everything"
```

With `pruning = "everything"` the node keeps no historical state, so queries at past heights fail. Restore your usual pruning after the node has caught up.

Things to know before you rely on it:

- **Rollback.** A DB written in deferred mode holds only the last flushed version, so its app state cannot be rolled back. Only CometBFT state can be rolled back, to a height at or above the last flushed version; the node then replays the blocks after the last flushed version on start. On v4 and later `rollback` does this by itself and refuses a lower target. On v1 to v3 `rollback` refuses and asks for `rollback --cometbft-only`. If you need app-state rollback, run with `deferred-persistence = "off"`.
- **Crashes.** A kill between flushes loses nothing that cannot be recomputed: on the next start the node repairs the store and CometBFT replays up to `deferred-flush-interval` blocks.
- **Shutdown.** On SIGINT or SIGTERM the store flushes before it exits. The default interval of 100 is small enough for that flush to finish before the multiplexer kills the child.
- **State sync.** A node started from a state sync snapshot may stay in per-block mode: activation can refuse when a store did not change in the snapshot's last block. A fix exists on an unmerged branch ([celestiaorg/iavl#4](https://github.com/celestiaorg/iavl/pull/4)). Syncing from genesis is not affected.
- **Memory.** Up to `deferred-flush-interval` blocks of new IAVL nodes stay in memory. On v4 and later about 4 million unflushed nodes (1 to 2 GB) force an earlier flush.

Every line accepts the same values, so the shared `app.toml` works for all eras. Any other value stops the binary that reads it:

- `deferred-persistence`: `"sync"`, `"off"` or `"always"`. `"always"` defers at the tip too and prunes the DB to one version on start (v4 and later). The v1 to v3 child has no such mode: it runs `"always"` as `"sync"` and logs that at start.
- `deferred-flush-interval`: `0` means the default (100). Any positive value is accepted. The v1 to v3 child runs values above 1000 as 1000 (it logs that at start), because it bounds memory only by the interval.

## Expected speeds

TODO: fill this table from measurements. Do not estimate. Record the hardware, disk type, config used and the date alongside the numbers.

| App version era | Height range | Blocks per second | Notes |
| --- | --- | --- | --- |
| v1 to v3 | TODO | TODO | TODO |
| v4 | TODO | TODO | TODO |
| v5 | TODO | TODO | TODO |
| v6 | TODO | TODO | TODO |
| v7 to v11 | TODO | TODO | TODO |
| v12 and later (native) | TODO | TODO | TODO |

Hardware used for the measurements: TODO.

## Hybrid path (verified changesets)

Instead of executing every block, the hybrid writes recorded store changes into the application DB, checks the app hash at every height against verified headers, and bootstraps CometBFT at the target height. Block sync then continues with full execution. All parts are draft pull requests and experimental: run them on machines you can rebuild and keep the `expected.tsv` and logs of every run.

1. **Get the changeset bundle.** A bundle of per-block changeset files with a signed `manifest.json`. `manifest verify` checks the ed25519 signature before parsing, then every file's size and sha256 ([celestia-app#8828](https://github.com/celestiaorg/celestia-app/pull/8828), [#8829](https://github.com/celestiaorg/celestia-app/pull/8829)). Where the bundle is hosted is a maintainer decision ([#8830](https://github.com/celestiaorg/celestia-app/pull/8830)).
2. **Verify headers.** `fshdr verify` anchors one header (a light-client trust root inside the trusting period, or a header hash you supply) and checks the hash chain down to height 1. `fshdr forward` checks up from a trusted header, including commits and validator sets. Both write `expected.tsv`: the trusted app hash after each height ([#8805](https://github.com/celestiaorg/celestia-app/pull/8805) to [#8807](https://github.com/celestiaorg/celestia-app/pull/8807)).
3. **Apply.** The appliers write the changesets and compare the app hash after every height with `expected.tsv`; a mismatch stops the run. There is one applier per store line: SDK v0.46 for app v1 to v3 ([cosmos-sdk#218](https://github.com/celestiaorg/cosmos-sdk/pull/218), [#231](https://github.com/celestiaorg/cosmos-sdk/pull/231) to [#233](https://github.com/celestiaorg/cosmos-sdk/pull/233)) and SDK v0.50 and later for app v4 and later ([cosmos-sdk#283](https://github.com/celestiaorg/cosmos-sdk/pull/283) to [#300](https://github.com/celestiaorg/cosmos-sdk/pull/300)).
4. **Bootstrap.** `fsboot bootstrap` at the target height (see [Block retention](#block-retention)). `scripts/fastsync.sh` runs steps 2 to 4 end to end ([#8811](https://github.com/celestiaorg/celestia-app/pull/8811)).
5. **Re-execute segments (optional).** For full verification, save checkpoint homes while applying and run a stock node over each segment `[c_k, c_k+1]` with `scripts/segment.sh`. Segments are independent and can run on separate machines ([#8831](https://github.com/celestiaorg/celestia-app/pull/8831) orchestrates this).

Trust model:

- You trust one root: a light-client trust height and hash (the same weak-subjectivity input as state sync), or one header hash obtained out of band from sources you trust. A trust root at genesis (the genesis validator set) is not implemented.
- You trust the binaries you built.
- RPC servers, block stores, changeset files, the bundle host and checkpoint buckets are untrusted; every value taken from them is checked.
- The appliers are strict by default: a height is applied only once a trusted header app hash for it is known. A recorded hash alone is not enough.
- After step 4 the application state has the committed app hash at every height, but the transitions were not executed locally. Execution bugs shared by +2/3 of validators would not be caught.
- Step 5 executes every transition with stock code and checks each segment's boundary headers against the verified ones.
- Proposer priorities and consensus params other than block max bytes and max gas are not in any header hash; `fsboot` trusts its source for them, as the stock command does.

## Disk space

A sync without retention keeps every block. Disk use during the sync can exceed the size of the data because of database compaction behaviour; this is being investigated in [celestia-core#3794](https://github.com/celestiaorg/celestia-core/issues/3794). Leave generous free disk headroom and watch usage during the sync.

## Halting at a height

If you set `halt-height`, the node exits with a consensus failure when it reaches that height. This is expected. Earlier releases could leave the embedded child running after that exit; see [celestia-app#8779](https://github.com/celestiaorg/celestia-app/issues/8779). If a `celestia-appd` process is still running after the node exited, stop it before you start the node again.

## After the node has caught up

Once the node is at the tip:

1. Restore `snapshot-interval` and `snapshot-keep-recent` if you serve state sync snapshots.
2. Restore `min-retain-blocks` if you turned retention off (the default is fine for most operators).
3. Re-enable the indexer if you need transaction or block event queries.
4. Restore `log_level` to your usual value.
5. Review the sync speedup options above and keep the defaults unless you have a reason to change them.
