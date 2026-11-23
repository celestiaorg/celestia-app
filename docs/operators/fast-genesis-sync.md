# Fast genesis sync

This guide is for operators who sync a node from genesis (or from a bootstrapped state) with the multiplexer build of `celestia-appd`. Syncing replays every app version era through the embedded binaries, so the settings below matter much more than they do at the tip.

> **Status: draft.** Measured speeds and the sync speedup options are marked `TODO` and will be filled in before this guide is final.

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

TODO: confirm the exact upgrade height and the version that re-enables deferred persistence once the sync speedups are merged.

## Inter-block cache

The multiplexer starts the v4 to v8 children with the inter-block cache disabled because of races with concurrent queries ([celestia-app#7750](https://github.com/celestiaorg/celestia-app/pull/7750)). A node that only syncs and serves no queries (RPC, gRPC, API all unused) can enable the cache safely; this measured about 21% faster. This is documentation only, not a bug: do not enable it on a node that serves queries.

TODO: document how to enable it for the embedded children (flag or `app.toml` key) once the sync speedup options are final.

## Sync speedups

The maintainers have decided that the sync speedups will be on by default, with a per-knob config option to disable the ones that carry risk. The option names are not final.

TODO: fill in the option table below.

| Option | Default | What it does | When to disable |
| --- | --- | --- | --- |
| TODO | TODO | TODO | TODO |

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
