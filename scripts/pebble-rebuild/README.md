# pebble-rebuild

Offline tool that **rebuilds a fragmented PebbleDB store** into a fresh one with
far fewer, larger sstables, by copying every key/value pair into a new store
opened with growing per-level target file sizes.

Use it when a node's PebbleDB has degenerated into millions of tiny sstables and
the per-file metadata Pebble holds at open is driving the process toward OOM.

> This is a hand-operated, one-off maintenance tool. Read the whole walkthrough
> once before running it against a real node — the individual steps are simple,
> but the ordering and the ownership step matter.

---

## Table of contents

1. [Why a rewrite (not compaction)](#1-why-a-rewrite-not-compaction)
2. [Before you start](#2-before-you-start)
3. [Step-by-step walkthrough](#3-step-by-step-walkthrough)
4. [Gotchas & lessons learned](#4-gotchas--lessons-learned)
5. [Worked example — a real ~9 TB store](#5-worked-example--a-real-9-tb-store)
6. [Flag reference](#6-flag-reference)

---

## 1. Why a rewrite (not compaction)

A multi-TB store opened with default Pebble options (`target_file_size = 2 MiB`,
single-threaded compaction) cannot keep itself compacted. It degenerates into
**millions of tiny sstables** — a real 4.4 TB blockstore reached **2.27M
sstables, 99.86% of them under 4 MB**. Pebble holds file metadata for every one
of those files, so opening the store costs multiple GB of RAM before the node
does anything.

You **cannot fix this in place**:

- `TargetFileSize` only affects **newly written** files.
- The fragmentation lives in the **bottom level (L6)**, whose files are
  non-overlapping and are therefore never rewritten by score-based compaction.
- Verified empirically: a full-range `db.Compact` on the 4.4 TB store ran for
  ~3.5 min, merged the upper levels, and left the ~2.18M tiny L6 files untouched.

The only way to collapse those files is to **rewrite the data** into a store
whose files are large from the start. That is what this tool does: stream every
key into a fresh store opened with growing target file sizes (8 → 128 MiB), so
the rewritten data lands in large files and the file count drops ~3–4×.

---

## 2. Before you start

**Match the node's Pebble major version.** This is the single most important
prerequisite. The store on disk was written by whatever Pebble the node's binary
links; the tool must use the **same major**, or it either cannot open the source
or writes a store the node cannot open.

- `celestia-appd` **v9.0.x** links **pebble v1** — this module pins **v1.1.5** to
  match it. That is the default here.
- `celestia-app` **v10+ / core v0.41+** links **pebble v2**. For a v2 node,
  rebuild the tool against `github.com/cockroachdb/pebble/v2` (change the import
  path in `main.go` + `go.mod`; the logic is unchanged).

Check what the target node links before you trust the default:

```sh
strings /usr/local/bin/celestia-appd | grep -oE 'cockroachdb/pebble(/v2)?@v[0-9.]+' | sort -u
```

**Disk headroom.** A rebuild holds the **source and destination at the same
time**, so you need free space ≈ the size of the single largest DB you rebuild.
The destination ends up ≈ the same size as the source (slightly smaller). During
the copy, disk usage transiently runs a bit above the final size because of WAL +
in-flight compaction files; it settles at the end. If space is tight, rebuild one
DB at a time and delete each `.old` before starting the next.

**Run long rebuilds in `tmux`.** A full rebuild of a multi-TB DB takes hours; run
it inside `tmux` so an SSH drop does not kill it.

---

## 3. Step-by-step walkthrough

This rebuilds a node's PebbleDB stores. Each `*.db` under the node's `data/`
directory (`blockstore.db`, `application.db`, `state.db`, `tx_index.db`, …) is a
**separate** Pebble store — rebuild them independently.

### Step 1 — Build the binary and copy it to the node

```sh
# on your machine, in this directory:
make build-linux                 # produces ./build/pebble-rebuild.linux (static)
scp build/pebble-rebuild.linux user@node:/home/user/
```

The Makefile builds with `CGO_ENABLED=0` (static, no libc/toolchain needed on the
node) and `GOWORK=off` (hermetic to this module).

### Step 2 — Probe first (a few minutes, changes nothing)

Before committing to a multi-hour run, do a small sample copy. It confirms three
things you want to know up front: that the tool **can open** the store (Pebble
version matches — look at `format=`), **how fragmented** it is, and the projected
**destination size + duration**.

```sh
# node must be stopped first (see Step 3) if you want to probe a live DB safely
./pebble-rebuild.linux -sample-gb 20 data/blockstore.db /data/tmp-probe
rm -rf /data/tmp-probe
```

Read the output:

```text
SOURCE: data/blockstore.db files=2302124 size=4506.6GiB (511 files/GiB) format=001
DEST:   /data/tmp-probe format=001 (...)
...
=== EXTRAPOLATION to full source (4507GiB) ===
files: source=2302124 rebuilt~=614323 reduction=4x
throughput=0.25GiB/s -> full rebuild ~= 5.0h
```

- `format=001` and no open error → the tool's Pebble can read this store. ✅
- `files/GiB` high (hundreds) → fragmented, worth rebuilding.
- Use the destination size (≈ source size) to confirm it fits your free space.

### Step 3 — Stop the node and prevent it from restarting

The store must be frozen (the node stopped) so all DBs sit at the same height,
and so the tool can take Pebble's exclusive lock.

```sh
sudo systemctl stop celestia-appd.service
sudo systemctl disable celestia-appd.service     # so a reboot can't start it mid-rebuild
pgrep -f celestia-appd || echo stopped
```

> `systemctl mask` does **not** work if the unit file lives in
> `/etc/systemd/system` (mask refuses to overwrite a real file). Use `disable`.

### Step 4 — Rebuild a DB

Run the full copy into a sibling `.new` directory, inside `tmux`:

```sh
tmux new -s rebuild
./pebble-rebuild.linux data/blockstore.db data/blockstore.db.new
```

You will see these **phases** (do not mistake the quiet ones for a hang):

1. **Copy** — a progress line every 15 s: `t=… copied=… keys=… dst_files=… rate=…`.
2. **`flushing dst (may trigger final compactions)...`** — a brief pause.
3. **Verify** — the tool reopens the destination read-only and re-scans **every
   key** to confirm the copy is faithful. On a huge DB this prints
   `verify: scanned N keys...` periodically; it can take many minutes.
4. **`VERIFY OK: dst keys=… == src`** — the copy is proven byte-faithful.
5. **`owner: … set to uid=… gid=…`** — the tool chowns the destination to match
   the source's owner (see the ownership gotcha below).

Detach with `Ctrl-b d`; reattach with `tmux attach -t rebuild`. Keep an eye on
disk in another pane: `watch -n 30 'df -h /data | tail -1'`.

**Do not proceed to swap until you see `VERIFY OK`.** If it prints
`VERIFY FAILED`, the source is untouched — investigate, do not swap.

### Step 5 — Swap the rebuilt DB in

The node is stopped, so this is instant and reversible (the old store becomes
`.old`):

```sh
cd data
mv blockstore.db blockstore.db.old
mv blockstore.db.new blockstore.db
```

If you are rebuilding several DBs in one downtime, rebuild + swap each, but
**start the node only once at the end** — all DBs must be at the same height,
which they are because the node was stopped the whole time.

### Step 6 — Start the node and verify

```sh
sudo systemctl enable --now celestia-appd.service
sudo journalctl -u celestia-appd.service -f      # watch it open the DBs and start
```

A clean start on the rebuilt store is itself the proof that the store is valid
and format-compatible. Confirm it reaches the tip and serves data:

```sh
curl -s localhost:26657/status | python3 -c 'import json,sys;s=json.load(sys.stdin)["result"]["sync_info"];print("h="+s["latest_block_height"],"catching_up="+str(s["catching_up"]))'
# for a block-results archival node, spot-check a few heights incl. old ones:
curl -s "localhost:26657/block_results?height=1000000" | head -c 80; echo
```

### Step 7 — (Optional) Certify a block-results archival node

For a node that must serve `/block_results`, prove there are **no gaps** across
the whole history with the `scan-blockresults` tool (built from
`celestia-core/cmd/scan-blockresults`). Run it on the **stopped** node:

```sh
sudo systemctl stop celestia-appd.service
sudo -u celestia ./scan-blockresults.linux --home /path/to/home --verify-every 50000
# -> "no gaps: every height in range has a stored block result"
sudo systemctl enable --now celestia-appd.service
```

(If `sudo -u celestia` needs a password you don't have, run it as root and then
`sudo chown -R celestia:celestia` the DBs it touched — same ownership gotcha.)

### Step 8 — Clean up

Only **after the node is healthy at the tip**:

```sh
rm -rf data/blockstore.db.old
```

Keep an **external snapshot/tarball** as the ultimate rollback until the node has
run stably for a day or two — `VERIFY OK` already guarantees the rebuilt store is
byte-identical to the old one, so the `.old` copies are low-value once the node is
up, but the snapshot protects against anything outside these DBs.

---

## 4. Gotchas & lessons learned

These are the things that actually bit us on a real node.

- **Ownership / running under `sudo` (the #1 gotcha).** A rebuild run as root
  creates a **root-owned** destination. The node runs as a non-root user
  (`celestia`), so it fails to open the store with `permission denied ... LOCK`
  and **crash-loops**. This tool now **auto-chowns the destination to the
  source's owner** at the end (the `owner: … set to …` line). If you disable that
  (`--match-owner=false`) or a tool run touched a DB some other way (e.g. running
  `scan-blockresults` as root), fix it manually:

  ```sh
  sudo chown -R celestia:celestia data/state.db data/blockstore.db
  ```

- **All DBs must be at the same height.** Rebuild only while the node is
  **stopped** — that freezes `blockstore.db`, `state.db`, `application.db` at one
  height, so the byte-faithful copies stay mutually consistent. Never mix a store
  rebuilt at height H1 with one at H2.

- **You can start incrementally.** A node runs fine on a *mixed* set (e.g. a
  rebuilt `blockstore.db` + untouched other DBs) — each store is independent and
  the copy is byte-faithful. You do not have to rebuild everything before
  starting.

- **Verify is silent and can be long.** The post-copy verify re-reads the entire
  destination. On a DB with billions of keys (a `tx_index.db`) this is tens of
  minutes. It is not hung — check `read_bytes` climbing and CPU:

  ```sh
  P=$(pgrep -f pebble-rebuild | tail -1); grep read_bytes /proc/$P/io; sleep 5; grep read_bytes /proc/$P/io
  ```

- **Disk is tightest near the end of the copy, not after.** During the copy,
  disk usage runs above the final size (WAL + obsolete compaction files); the
  final flush cleans it up. If free space dips low, the tool aborts cleanly (the
  source is read-only — no corruption, you just restart from scratch).

- **Expect ~3–4× fewer files, not ~60×.** The tool relies on background
  compaction during the write; it does not force a full compaction to a single
  L6. That is enough to cut the open-time metadata memory several-fold, which is
  the goal.

- **`tx_index.db` is special.** It can hold *billions* of keys and push the
  tool's RSS to tens of GB during the copy (fine on a large box). Its verify is
  the longest.

- **`mask` won't work** for a unit whose file is in `/etc/systemd/system`; use
  `disable` to keep it from starting during the rebuild, and
  `enable --now` to bring it back.

---

## 5. Worked example — a real ~9 TB store

A mainnet archival node (`celestia-appd v9.0.8`, pebble v1.1.5) with ~9 TB of
PebbleDB stores on a 14 TB `/data`, rebuilt end-to-end. Node stopped throughout;
DBs swapped and started once.

| DB | sstables before | after | reduction | size | copy time | notes |
|----|----------------:|------:|:---------:|-----:|:---------:|-------|
| blockstore.db  | 2,302,124 | 594,238 | ~4×   | 4.5 TB | 6h37m | ~192 M keys |
| application.db | 1,549,414 | 452,925 | ~3.4× | 3.3 TB | ~3.7h | IAVL / SDK store |
| state.db       |   256,290 |  73,794 | ~3.5× | 490 GB | ~1h   | holds block_results |
| tx_index.db    |   443,364 | 133,255 | ~3.3× | 912 GB | 2h13m | **10.1 B keys**, RSS ~30 GB |
| **total**      | **~4.55M** | **~1.25M** | | | | open-time metadata cut ~3.6× |

After swap + one start, the node came up at the tip and served `/block_results`
across the full history. A `scan-blockresults --verify-every 50000` over all
14,161,898 heights reported **`no gaps`** with **283 sampled results, 0 failed**.

---

## 6. Flag reference

```text
pebble-rebuild [flags] <src-db> <dst-db>

  -sample-gb float    stop after N GiB — MEASUREMENT ONLY, produces an INCOMPLETE
                      store and disables --verify (default 0 = full copy)
  -verify             re-scan dst and assert it matches src after a full copy (default true)
  -match-owner        chown dst to the source's uid/gid after a full copy, so a
                      sudo run yields a store the node's user can open (default true)
  -force              allow a non-empty destination directory (default: refuse)
  -batch-mb int       flush the write batch at this size (default 64)
  -progress-sec int   progress report interval, copy and verify (default 15)
```

Build targets: `make build` (host), `make build-linux` (static linux/amd64),
`make test`, `make clean`. Self-contained module — the Makefile sets `GOWORK=off`
and `CGO_ENABLED=0`.
