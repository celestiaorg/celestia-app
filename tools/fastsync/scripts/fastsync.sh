#!/usr/bin/env bash
# fastsync.sh: changesets -> verified application.db at H -> CometBFT stores at H.
#
#   fastsync.sh --home DIR --changesets DIR --to H --rpc URL[,URL...] \
#               (--trust-height T --trust-hash HEX | --anchor-height A --anchor-hash HEX) [options]
#
# Steps (each aborts the run on failure; nothing is bootstrapped unless every applied height verified):
#   0. preflight: config and genesis present, CometBFT stores empty, chain id and db backend read.
#   1. fshdr verify: trusted app hashes for --from..H in <home>/fastsync/expected.tsv, from headers
#      hash-chained down from a light-client anchor (trust root) or an operator anchor (--anchor-hash).
#   2. apply changesets to application.db with the applier (--applier, SDK v0.46 stores); when it
#      reports HANDOVER (the first block written by the SDK v0.50 store), continue with --applier50.
#      Every height must match expected.tsv. With --checkpoints, the apply stops at each checkpoint c
#      and a bootstrapped node home is saved in <home>/fastsync/checkpoints/c for segment.sh.
#   3. optional --fastindex: build the IAVL fast-node index offline.
#   4. fsboot bootstrap: CometBFT state and block store at H, checked against expected.tsv.
#   5. print the start command. On start, the CometBFT handshake checks that the application's
#      height and app hash equal the state at H.
#
# Applier interface (see README): `<applier> apply -db DIR -backend B -changesets DIR -expected FILE
# -require-trusted -to H [-parallel]`; exit 2 means an app hash mismatch; output lines "HANDOVER ..."
# and "done ... to height N ... recorded hash only: 0".
set -euo pipefail

HOME_DIR='' CS_DIR='' RPC='' TRUST_H=0 TRUST_HASH='' TRUST_PERIOD=168h FROM=1 TO=0
ANCHOR_H=0 ANCHOR_HASH='' BOOT_SRC_DATA='' HDR_BLOCKSTORE='' START_FLAGS='' CHECKPOINTS=''
PARALLEL=1 FASTINDEX=0 DRY_RUN=0 SINGLE_SOURCE=0
APPD=${APPD:-celestia-appd} FSHDR=${FSHDR:-fshdr} FSBOOT=${FSBOOT:-fsboot}
APPLIER=${APPLIER:-} APPLIER50=${APPLIER50:-} FASTINDEX46=${FASTINDEX46:-fastindex46} FASTINDEX50=${FASTINDEX50:-fastindex}

usage() {
  sed -n '2,6p' "$0"
  cat <<'EOF2'
options: --from N (default 1)  --trust-period 168h  --header-blockstore DIR (headers from a local block store)
         --boot-src-data DIR (fsboot: validator sets, commit and params from a stopped node you run and trust
           for proposer priorities and unhashed params, instead of RPC)
         --allow-single-source (fsboot from one --rpc server, which is then trusted for those fields)
         --checkpoints h1,h2,...  (homes for parallel re-execution segments)
         --fastindex  --no-parallel  --start-flags "..."  --dry-run (print the commands, run nothing)
tools:   --appd PATH --fshdr PATH --fsboot PATH --applier PATH --applier50 PATH (or the env vars APPD, FSHDR,
         FSBOOT, APPLIER, APPLIER50, FASTINDEX46, FASTINDEX50)
EOF2
  exit 1
}
while [ $# -gt 0 ]; do
  case $1 in
    --home) HOME_DIR=$2; shift ;;
    --changesets) CS_DIR=$2; shift ;;
    --rpc) RPC=$2; shift ;;
    --trust-height) TRUST_H=$2; shift ;;
    --trust-hash) TRUST_HASH=$2; shift ;;
    --trust-period) TRUST_PERIOD=$2; shift ;;
    --anchor-height) ANCHOR_H=$2; shift ;;
    --anchor-hash) ANCHOR_HASH=$2; shift ;;
    --from) FROM=$2; shift ;;
    --to) TO=$2; shift ;;
    --header-blockstore) HDR_BLOCKSTORE=$2; shift ;;
    --boot-src-data) BOOT_SRC_DATA=$2; shift ;;
    --checkpoints) CHECKPOINTS=$2; shift ;;
    --allow-single-source) SINGLE_SOURCE=1 ;;
    --start-flags) START_FLAGS=$2; shift ;;
    --appd) APPD=$2; shift ;;
    --fshdr) FSHDR=$2; shift ;;
    --fsboot) FSBOOT=$2; shift ;;
    --applier) APPLIER=$2; shift ;;
    --applier50) APPLIER50=$2; shift ;;
    --fastindex) FASTINDEX=1 ;;
    --no-parallel) PARALLEL=0 ;;
    --dry-run) DRY_RUN=1 ;;
    -h | --help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
  shift
done

log() { echo "[fastsync $(date -u +%H:%M:%S)] $*"; }
die() { echo "[fastsync] ABORT: $*" >&2; exit 1; }
# run CMD...: execute, or print it with --dry-run.
run() {
  if [ "$DRY_RUN" = 1 ]; then echo "+ $*"; else "$@"; fi
}
is_uint() { [[ $1 =~ ^[0-9]+$ ]]; }

[ -n "$HOME_DIR" ] && [ -n "$CS_DIR" ] && [ -n "$RPC$HDR_BLOCKSTORE" ] || usage
is_uint "$TO" && is_uint "$FROM" && [ "$TO" -ge "$FROM" ] && [ "$FROM" -ge 1 ] || die "need 1 <= --from <= --to"
if ! { is_uint "$TRUST_H" && is_uint "$ANCHOR_H"; }; then die "heights must be numbers"; fi
if [ -n "$ANCHOR_HASH" ]; then
  [ "$ANCHOR_H" -gt "$TO" ] || die "--anchor-hash needs --anchor-height > --to"
else
  [ "$TRUST_H" -gt 0 ] && [ -n "$TRUST_HASH" ] && [ -n "$RPC" ] || die "need --rpc, --trust-height and --trust-hash (or --anchor-height and --anchor-hash)"
fi
[ -n "$APPLIER" ] || die "--applier is required"
[ -n "$RPC" ] || [ -n "$BOOT_SRC_DATA" ] || die "fsboot needs --rpc or --boot-src-data"
distinct_rpcs() { tr ',' '\n' <<< "$RPC" | sed 's#/*$##' | grep . | sort -u | wc -l; }
if [ -z "$BOOT_SRC_DATA" ] && [ "$SINGLE_SOURCE" = 0 ] && [ "$(distinct_rpcs)" -lt 2 ]; then
  die "fsboot needs two distinct --rpc servers: one alone is trusted for proposer priorities and unhashed params (--allow-single-source accepts that)"
fi

H=$TO
CFG=$HOME_DIR/config/config.toml GEN=$HOME_DIR/config/genesis.json WORK=$HOME_DIR/fastsync

# ---- 0. preflight
[ -f "$CFG" ] && [ -f "$GEN" ] || die "need $CFG and $GEN (run '$APPD init' and install the genesis first)"
CHAIN_ID=$(grep -o '"chain_id": *"[^"]*"' "$GEN" | head -1 | sed 's/.*"\([^"]*\)"$/\1/')
[ -n "$CHAIN_ID" ] || die "no chain_id in $GEN"
BACKEND=$(sed -n 's/^db_backend *= *"\(.*\)"/\1/p' "$CFG" | head -1)
BACKEND=${BACKEND:-goleveldb}
for d in state.db blockstore.db; do
  if [ -d "$HOME_DIR/data/$d" ] && [ -n "$(ls -A "$HOME_DIR/data/$d" 2>/dev/null)" ]; then
    die "$HOME_DIR/data/$d is not empty: the bootstrap needs empty CometBFT stores (application.db may stay for a resume)"
  fi
done
[ ! -f "$WORK/POISONED" ] || die "$(cat "$WORK/POISONED"); application.db holds an unverified height: delete it (or restore a checkpoint copy and its applied-to marker) and $WORK/POISONED, then rerun"
run mkdir -p "$WORK"
log "chain=$CHAIN_ID backend=$BACKEND target H=$H from=$FROM"

# ---- 1. trusted app hashes
HDR_ARGS=(verify -chain-id "$CHAIN_ID" -from "$FROM" -to "$H" -out "$WORK/expected.tsv")
[ -n "$RPC" ] && HDR_ARGS+=(-rpc "$RPC")
[ -n "$HDR_BLOCKSTORE" ] && HDR_ARGS+=(-blockstore "$HDR_BLOCKSTORE" -backend "$BACKEND")
[ "$ANCHOR_H" -gt 0 ] && HDR_ARGS+=(-anchor-height "$ANCHOR_H")
if [ -n "$ANCHOR_HASH" ]; then
  HDR_ARGS+=(-anchor-hash "$ANCHOR_HASH")
else
  # an old target: anchor at the (recent) trust height and hash-chain down
  [ "$ANCHOR_H" = 0 ] && [ "$TRUST_H" -gt $((H + 1)) ] && HDR_ARGS+=(-anchor-height "$TRUST_H")
  HDR_ARGS+=(-trust-height "$TRUST_H" -trust-hash "$TRUST_HASH" -trust-period "$TRUST_PERIOD")
fi
run "$FSHDR" "${HDR_ARGS[@]}" || die "header verification failed"

# ---- 2. apply (v0.46 applier, then the v0.50 applier after a HANDOVER), with checkpoints
CUR=$APPLIER
APPLIED=$(cat "$WORK/applied-to" 2>/dev/null || echo 0) # resume marker, written only after a verified apply
apply_to() { # $1 = target: applies (APPLIED, target], every height checked against expected.tsv
  local tgt=$1 rc done_line args
  args=(apply -db "$HOME_DIR/data" -backend "$BACKEND" -changesets "$CS_DIR" -expected "$WORK/expected.tsv" -require-trusted -to "$tgt")
  [ "$PARALLEL" = 1 ] && args+=(-parallel)
  [ "$tgt" -le "$APPLIED" ] && return 0
  if [ "$DRY_RUN" = 1 ]; then
    run "$CUR" "${args[@]}"
    APPLIED=$tgt
    return 0
  fi
  while true; do
    log "apply to $tgt: $CUR ${args[*]}"
    set +e
    "$CUR" "${args[@]}" > "$WORK/apply-$tgt.log" 2>&1
    rc=$?
    set -e
    grep -v '^height .*applied=' "$WORK/apply-$tgt.log" | sed 's/^/    /' || true
    if [ "$rc" = 2 ]; then # the applier committed the bad height before comparing: never resume this DB
      echo "APP HASH MISMATCH while applying to $tgt with $CUR" > "$WORK/POISONED"
      die "APP HASH MISMATCH during apply (see $WORK/apply-$tgt.log); nothing bootstrapped; DB marked POISONED"
    fi
    [ "$rc" = 0 ] || die "$CUR apply failed (rc=$rc)"
    if grep -q '^HANDOVER' "$WORK/apply-$tgt.log" && [ "$CUR" = "$APPLIER" ]; then
      [ -n "$APPLIER50" ] || die "the changesets continue with app v4+ blocks: rerun with --applier50 PATH"
      CUR=$APPLIER50
      log "handover to the v0.50 applier ($CUR)"
      continue
    fi
    break
  done
  done_line=$(grep '^done' "$WORK/apply-$tgt.log" | tail -1)
  [ "$(sed -n 's/.*to height \([0-9]*\).*/\1/p' <<< "$done_line")" = "$tgt" ] || die "applier stopped at '$done_line', expected $tgt"
  grep -q 'recorded hash only: 0' <<< "$done_line" || die "some heights were not verified against trusted headers"
  APPLIED=$tgt
  echo "$tgt" > "$WORK/applied-to"
}
bootstrap_home() { # $1 = home, $2 = height, [$3 = app hash of the home's application DB at $2]
  local args=(bootstrap -home "$1" -height "$2" -expected "$WORK/expected.tsv")
  if [ -n "$BOOT_SRC_DATA" ]; then args+=(-src-data "$BOOT_SRC_DATA"); else args+=(-rpc "$RPC"); fi
  [ "$SINGLE_SOURCE" = 1 ] && args+=(-allow-single-source)
  [ -n "${3:-}" ] && args+=(-app-hash "$3")
  if [ "$DRY_RUN" = 1 ]; then run "$FSBOOT" "${args[@]}"; return 0; fi
  run mkdir -p "$1/fastsync"
  "$FSBOOT" "${args[@]}" > "$1/fastsync/bootstrap.log" 2>&1 || { tail -5 "$1/fastsync/bootstrap.log"; return 1; }
  grep '^\[fsboot' "$1/fastsync/bootstrap.log" | sed 's/^/    /'
}
make_checkpoint() { # $1 = height: a self-contained node home at $1 (app DB copy + bootstrapped CometBFT stores)
  local ck=$1 d=$WORK/checkpoints/$1 app_hash
  run rm -rf "$d"
  run mkdir -p "$d"
  # multiplexer homes: reuse the embedded child binaries instead of extracting them again
  [ -d "$HOME_DIR/bin" ] && run cp -rs "$(cd "$HOME_DIR/bin" && pwd)" "$d/bin"
  run "$APPD" init "ckpt-$ck" --chain-id "$CHAIN_ID" --home "$d" # fresh node keys
  run cp "$GEN" "$CFG" "$HOME_DIR/config/app.toml" "$d/config/"
  run cp -a "$HOME_DIR/data/application.db" "$d/data/" # the applier exited: the DB is closed
  app_hash=$(awk -v h="$ck" -F'\t' '$1==h {print $2}' "$WORK/expected.tsv" 2>/dev/null || true)
  bootstrap_home "$d" "$ck" "$app_hash" || die "bootstrap of checkpoint $ck failed (see $d/fastsync/bootstrap.log)"
  if [ "$DRY_RUN" = 0 ]; then
    awk -v h="$ck" -F'\t' '$1==h {print "height\t"$1"\napp_hash_after\t"$2"\nnext_header_hash\t"$3}' "$WORK/expected.tsv" > "$d/CHECKPOINT"
  fi
  log "checkpoint $ck -> $d"
}
CKS=${CHECKPOINTS//[, ]/$'\n'}
for ck in $(printf '%s\n' "$CKS" | grep -E '^[0-9]+$' | sort -n -u); do
  if [ "$ck" -lt "$FROM" ] || [ "$ck" -ge "$H" ]; then log "skip checkpoint $ck (outside $FROM..$((H - 1)))"; continue; fi
  if [ "$ck" -le "$APPLIED" ]; then # resume: application.db is already past ck
    if [ -f "$WORK/checkpoints/$ck/CHECKPOINT" ]; then log "checkpoint $ck exists"; else log "skip checkpoint $ck: application.db is already at $APPLIED"; fi
    continue
  fi
  apply_to "$ck"
  make_checkpoint "$ck"
done
apply_to "$H"
log "application.db at H=$H; every applied height matched expected.tsv"

# ---- 3. fast-node index
if [ "$FASTINDEX" = 1 ]; then
  if [ "$CUR" = "$APPLIER" ]; then FI=$FASTINDEX46; else FI=$FASTINDEX50; fi
  run env -u CELESTIA_IAVL_FLUSH_EVERY "$FI" -db "$HOME_DIR/data" -backend "$BACKEND" || die "fastindex failed"
fi

# ---- 4. CometBFT stores at H
bootstrap_home "$HOME_DIR" "$H" || die "bootstrap failed (see $WORK/bootstrap.log)"
[ "$DRY_RUN" = 1 ] || echo "$H" > "$WORK/height"
if [ -d "$WORK/checkpoints" ]; then
  log "checkpoints for re-execution segments (scripts/segment.sh): $(find "$WORK/checkpoints" -mindepth 1 -maxdepth 1 -printf '%f\n' | sort -n | tr '\n' ' ')"
fi

# ---- 5. hand-off
log "DONE. application.db and the CometBFT stores are at height $H. Start the node (block sync from $((H + 1))):"
echo "    $APPD start --home $HOME_DIR $START_FLAGS"
[ "$FASTINDEX" = 1 ] || log "NOTE: no --fastindex: the node rebuilds the IAVL fast-node index during its first start (minutes or more on mainnet state)."
