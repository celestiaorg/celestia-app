# Fast genesis sync

This guide is for operators who sync a node from genesis (or from a bootstrapped state) with the multiplexer build of `celestia-appd`. Syncing replays every app version era through the embedded binaries, so the settings below matter much more than they do at the tip.

> **Status: draft.** Measured speeds are marked `TODO` and will be filled in before this guide is final. The sync speedup options describe open pull requests and can change before release.

## Quick checklist

1. Edit the config files before the first start (see [Recommended config](#recommended-config)).
2. Run the node under systemd with `KillMode=mixed` (see [Run under systemd](#run-under-systemd)).
3. Keep block retention off until the node is past the v4 upgrade, or start from an empty blockstore (see [Block retention](#block-retention)).
4. Restart the node once after the v4 upgrade (see [Restart after the v4 upgrade](#restart-after-the-v4-upgrade)).
5. Switch to the normal tip configuration once the node has caught up.

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

## Block retention

The default `min-retain-blocks` is 9750 (`2 * SnapshotInterval`, see [`pkg/appconsts/initial_consts.go`](../../pkg/appconsts/initial_consts.go)). The first embedded child that returns a retain height is the v4 binary. At that point the parent prunes every block below the retain height synchronously inside block processing. After a long sync that is millions of blocks and the node stalls for hours (see [celestia-core#3793](https://github.com/celestiaorg/celestia-core/issues/3793)).

Pick one:

- **Retention off until past v4.** Set `min-retain-blocks = 0` in `config/app.toml` and restore it after the node has moved past the v4 upgrade height. Pruning then starts from a small backlog.
- **Start from an empty blockstore.** Bootstrap the node at a recent height instead of replaying history. TODO: document the exact bootstrap command for the current release; the Go API is `node.BootstrapState` in celestia-core, and this guide does not yet have a verified CLI recipe.

Note that the start hook only raises a non-zero `min-retain-blocks` to the minimum; `0` is left alone.

## Restart after the v4 upgrade

Some work that is deferred during the early eras (deferred persistence in the stores) is re-enabled only when the process starts on a version that supports it. Restart the node once after it crosses the v4 upgrade height. With `KillMode=mixed` this is a clean `systemctl restart celestia-appd`.

TODO: confirm whether this restart is still needed and why. In the open pull requests, deferred persistence exists only for v1 to v3 and for v7 and later (see [Which eras get which speedup](#which-eras-get-which-speedup)), not for the v4 child, and each child decides it when it starts, so the reason given above does not match the code.

## Inter-block cache

The multiplexer starts the v4 to v8 children with the inter-block cache disabled because of races with concurrent queries ([celestia-app#7750](https://github.com/celestiaorg/celestia-app/pull/7750)). A node that only syncs and serves no queries (RPC, gRPC, API all unused) can enable the cache safely; this measured about 21% faster. This is documentation only, not a bug: do not enable it on a node that serves queries.

TODO: document how to enable it for the embedded children (flag or `app.toml` key) once the sync speedup options are final.

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

TODO: confirm the patched children embedded in the release this guide ships with.

### `config/config.toml`

| Option | Default | What it does | When to disable |
| --- | --- | --- | --- |
| `parallel_commit_verify` (top level) | `true` | Verifies the signatures of large commits on several cores. Results are identical to sequential verification. | If it causes CPU contention with other processes on the host. |
| `[blocksync] pipeline` | `true` | Prepares the next block (part set and commit verification) while the current one is applied. The synced blocks and state are identical. | If you suspect a blocksync problem; `false` prepares each block after the previous one is applied. |
| `[blocksync] max_requesters` | `40` | Number of blocks requested ahead of the one being applied, in `[2, 1000]`; `0` means the default. Worst-case memory is `max_requesters` times the maximum block size. | Lower it on hosts with little memory. Raising it helps only when peers are slow. |

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
- The node keeps no history that deferred mode would delete. On v4 and later every IAVL tree must hold a single version (a fresh or deferred-written DB). On v1 to v3, pruning must not keep more recent versions than `deferred-flush-interval`, state sync snapshots must be off (`snapshot-interval = 0`) and IAVL lazy loading must be off.

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
