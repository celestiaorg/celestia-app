# App Hash Mismatch Debug Collection

This document describes what to collect when a node reports an app hash
mismatch. The goal is to preserve enough local evidence to reproduce the
divergence before rollback, resync, pruning, or manual cleanup destroys it.

## Summary

An app hash mismatch is usually reported as:

```text
wrong Block.Header.AppHash. Expected <LOCAL_APP_HASH>, got <BLOCK_HEADER_APP_HASH>
```

The mismatch is detected while validating block `H`, but the divergent state was
computed while applying block `H-1`. Collect data for `H-2`, `H-1`, and `H`.

Do not run `celestia-appd rollback`, resync, or delete files until the archive
below has been created.

## Set Variables

Replace the placeholders with values from the mismatch log line.

```sh
H=<MISMATCH_HEIGHT>
CELESTIA_HOME=<NODE_HOME>
SERVICE=celestia-appd
RPC=${RPC:-tcp://localhost:26657}
OUT=/tmp/celestia-apphash-$H-$(date -u +%Y%m%dT%H%M%SZ)

mkdir -p "$OUT/commands" "$OUT/data" "$OUT/config"
```

`CELESTIA_HOME` is usually `~/.celestia-app`.

## Collect Best-Effort RPC Data

Run these before stopping the node if the local RPC is still available. If the
node already stopped, run them anyway and keep the error output. The failure mode
is useful context.

```sh
celestia-appd status --node "$RPC" --output json \
  > "$OUT/commands/status.json" 2>&1

celestia-appd query block --type height $((H-2)) --node "$RPC" -o json \
  > "$OUT/commands/block-$((H-2)).json" 2>&1
celestia-appd query block --type height $((H-1)) --node "$RPC" -o json \
  > "$OUT/commands/block-$((H-1)).json" 2>&1
celestia-appd query block --type height "$H" --node "$RPC" -o json \
  > "$OUT/commands/block-$H.json" 2>&1

celestia-appd query block-results $((H-2)) --node "$RPC" -o json \
  > "$OUT/commands/block-results-$((H-2)).json" 2>&1
celestia-appd query block-results $((H-1)) --node "$RPC" -o json \
  > "$OUT/commands/block-results-$((H-1)).json" 2>&1
celestia-appd query block-results "$H" --node "$RPC" -o json \
  > "$OUT/commands/block-results-$H.json" 2>&1

celestia-appd query txs --query "tx.height=$((H-1))" --node "$RPC" -o json \
  > "$OUT/commands/txs-$((H-1)).json" 2>&1
```

## Save Logs

For systemd:

```sh
journalctl -u "$SERVICE" --since "24 hours ago" > "$OUT/node.log" 2>&1
```

If the node logs to a file instead of journald, copy that file:

```sh
cp -a <NODE_LOG_FILE> "$OUT/node.log"
```

If the node was started with `--trace-store <path>`, copy that trace file too.
It is the best source for KVStore state transitions:

```sh
cp -a <TRACE_STORE_FILE> "$OUT/trace-store.log"
```

If `--trace-store` was not enabled before the mismatch, full KVStore state
transitions cannot be recovered from normal logs.

## Stop The Node

Stop the process before copying databases so the copies are consistent.

For systemd:

```sh
sudo systemctl stop "$SERVICE"
```

If you are not using systemd, stop the `celestia-appd` process with your normal
process manager.

## Preserve Databases

It's important to have all the data folder to be able to reproduce the mismatch and debug it:

```sh
cp -a "$CELESTIA_HOME/data" "$OUT/data"
```

## Preserve Safe Config Files

Copy only non-key config files.

```sh
cp -a "$CELESTIA_HOME/config/config.toml" "$OUT/config/"
cp -a "$CELESTIA_HOME/config/app.toml" "$OUT/config/"
cp -a "$CELESTIA_HOME/config/client.toml" "$OUT/config/"
cp -a "$CELESTIA_HOME/config/genesis.json" "$OUT/config/"
```

Do not copy:

- `config/priv_validator_key.json`
- `config/node_key.json`

## Collect Local Diagnostics

Run the state inspection commands against the frozen copy in `OUT`.

```sh
celestia-appd debug module-hash-by-height $((H-2)) --home "$OUT" -o json \
  > "$OUT/commands/module-hashes-$((H-2)).json" 2>&1
celestia-appd debug module-hash-by-height $((H-1)) --home "$OUT" -o json \
  > "$OUT/commands/module-hashes-$((H-1)).json" 2>&1
celestia-appd debug module-hash-by-height "$H" --home "$OUT" -o json \
  > "$OUT/commands/module-hashes-$H.json" 2>&1

celestia-appd debug check-version $((H-2)) --home "$OUT" \
  > "$OUT/commands/check-version-$((H-2)).txt" 2>&1
celestia-appd debug check-version $((H-1)) --home "$OUT" \
  > "$OUT/commands/check-version-$((H-1)).txt" 2>&1
celestia-appd debug check-version "$H" --home "$OUT" \
  > "$OUT/commands/check-version-$H.txt" 2>&1
```

The `H` commands may fail because the node usually only committed local state
through `H-1`. Keep the output anyway.

Export app state at `H-1` if the historical version is available. This can be
large and slow.

```sh
celestia-appd export --height $((H-1)) --home "$OUT" \
  --output-document "$OUT/commands/export-$((H-1)).json" \
  > "$OUT/commands/export-$((H-1)).log" 2>&1
```

## Package The Evidence

```sh
tar -C "$(dirname "$OUT")" -czf "$OUT.tar.gz" "$(basename "$OUT")"

sha256sum "$OUT.tar.gz" > "$OUT.tar.gz.sha256" 2>/dev/null || \
  shasum -a 256 "$OUT.tar.gz" > "$OUT.tar.gz.sha256"
```

Before sharing, verify that the archive does not contain private keys:

```sh
tar -tzf "$OUT.tar.gz" | grep -E 'priv_validator_key.json|node_key.json'
```

The command above should print nothing.

## Send To The Celestia Team

Send:

- `$OUT.tar.gz`
- `$OUT.tar.gz.sha256`
- the original app hash mismatch log line

The archive should contain:

- `data`
- recent node logs
- `trace-store.log`, if tracing was enabled before the mismatch
- block, block-results, and tx query output if RPC was available
- module hashes for `H-2`, `H-1`, and `H`
- IAVL version checks for `H-2`, `H-1`, and `H`
- app export for `H-1`, if available
- binary version info

Only run rollback or resync after this archive has been created.
