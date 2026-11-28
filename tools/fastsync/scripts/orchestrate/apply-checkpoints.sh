#!/usr/bin/env bash
# apply-checkpoints.sh: apply VM of the multi-VM "fully verified" mode. Replays the changeset bundle into
# application.db and, every N blocks, publishes a checkpoint (app DB tarball + sha256 + height + app hash)
# to a bucket, from which segment VMs (segment-run.sh) start their re-execution.
#
#   apply-checkpoints.sh --home DIR --changesets DIR --expected expected.tsv --chain-id ID --bucket gs://B/prefix
#                        (--every 250000 --to H | --checkpoints h1,h2,...)
#                        (--manifest manifest.json --pub bundle.pub [--sig manifest.json.sig] | --skip-bundle-verify)
#                        [--applier PATH] [--applier50 PATH] [--manifest-bin PATH] [--backend B] [--work DIR]
#                        [--no-parallel] [--dry-run]
#
# --home        an initialized node home (genesis + config.toml); <home>/data/application.db is the applier's
#               target. It may already hold a partial apply: <work>/applied-to records the last verified height.
# --expected    fshdr verify output (h, app hash after h, hash of header h+1, ...) covering 1..max checkpoint.
#               Every applied height is checked against it (-require-trusted): a checkpoint is only published
#               after the applier verified its app hash against the hash-chain-verified headers.
# --bucket      gs:// prefix; layout in lib.sh. A checkpoint is complete once its CHECKPOINT object exists
#               (uploaded last); completed checkpoints are skipped on a rerun.
#
# --applier/--applier50 use the applier interface of ../fastsync.sh (SDK v0.46 then v0.50 stores, HANDOVER line,
# final "done ... to height N ... recorded hash only: 0"). Exit status 2 = app hash mismatch: the DB holds an
# unverified height, so it is marked POISONED and never resumed or published.
# Why not fastsync.sh --checkpoints: that saves bootstrapped homes on local disk and only returns after the last
# height; here each checkpoint is published as soon as it is applied, so segment VMs start while the apply runs.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
LOG_TAG=apply-checkpoints
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$HERE/lib.sh"

HOME_DIR='' CS_DIR='' EXPECTED='' CHAIN_ID='' BUCKET='' EVERY=250000 TO='' CHECKPOINTS='' BACKEND='' WORK=''
MANIFEST='' SIG='' PUB='' SKIP_VERIFY=0 PARALLEL=1
APPLIER=${APPLIER:-} APPLIER50=${APPLIER50:-} MANIFEST_BIN=${MANIFEST_BIN:-manifest}
usage() { sed -n 5,10p "$0" >&2; exit 2; }
while [ $# -gt 0 ]; do
  case $1 in
    --home) HOME_DIR=$2; shift;; --changesets) CS_DIR=$2; shift;; --expected) EXPECTED=$2; shift;;
    --chain-id) CHAIN_ID=$2; shift;; --bucket) BUCKET=${2%/}; shift;; --every) EVERY=$2; shift;; --to) TO=$2; shift;;
    --checkpoints) CHECKPOINTS=$2; shift;; --backend) BACKEND=$2; shift;; --work) WORK=$2; shift;;
    --manifest) MANIFEST=$2; shift;; --sig) SIG=$2; shift;; --pub) PUB=$2; shift;; --skip-bundle-verify) SKIP_VERIFY=1;;
    --applier) APPLIER=$2; shift;; --applier50) APPLIER50=$2; shift;; --manifest-bin) MANIFEST_BIN=$2; shift;;
    --no-parallel) PARALLEL=0;; --dry-run) DRY_RUN=1;; -h|--help) usage;;
    *) echo "unknown arg $1" >&2; usage;;
  esac
  shift
done
[ -n "$HOME_DIR" ] && [ -n "$CS_DIR" ] && [ -n "$EXPECTED" ] && [ -n "$CHAIN_ID" ] && [ -n "$BUCKET" ] || usage
[ -n "$APPLIER" ] || die "--applier is required"
check_bucket "$BUCKET"
[[ $CHAIN_ID =~ ^[A-Za-z0-9._-]{1,64}$ ]] || die "bad --chain-id '$CHAIN_ID'"
WORK=${WORK:-$HOME_DIR/fastsync-orch}
SIG=${SIG:-$MANIFEST.sig}

# ---- checkpoint list
if [ -n "$CHECKPOINTS" ]; then
  mapfile -t CKS < <(tr ',' '\n' <<< "$CHECKPOINTS" | sed '/^$/d')
else
  [ -n "$TO" ] || die "give --to (with --every) or --checkpoints"
  need_uint --every "$EVERY"; need_uint --to "$TO"; [ "$EVERY" -ge 1 ] || die "--every must be >= 1"
  [ $((TO / EVERY)) -le 100000 ] || die "too many checkpoints: raise --every"
  mapfile -t CKS < <(seq "$EVERY" "$EVERY" "$TO")
fi
for c in "${CKS[@]}"; do need_uint checkpoint "$c"; [ "$c" -ge 1 ] || die "checkpoint heights must be >= 1"; done
mapfile -t CKS < <(printf '%s\n' "${CKS[@]}" | sort -n -u)
[ "${#CKS[@]}" -gt 0 ] || die "no checkpoints"
MAXCK=${CKS[${#CKS[@]}-1]}
log "chain=$CHAIN_ID checkpoints=${#CKS[@]} (${CKS[0]}..$MAXCK) bucket=$BUCKET dry_run=$DRY_RUN"

# ---- preflight (skipped in dry runs where inputs may not exist)
if [ "$DRY_RUN" != 1 ]; then
  need_tools tar zstd sha256sum awk "${GCLOUD_CMD[0]}" "$APPLIER"
  CFG=$HOME_DIR/config/config.toml GEN=$HOME_DIR/config/genesis.json
  [ -f "$CFG" ] && [ -f "$GEN" ] || die "need $CFG and $GEN"
  GEN_CHAIN=$(grep -o -m1 '"chain_id": *"[^"]*"' "$GEN" | head -1 | sed 's/.*"\([^"]*\)"$/\1/' || true)
  [ "$GEN_CHAIN" = "$CHAIN_ID" ] || die "genesis chain_id '$GEN_CHAIN' != --chain-id '$CHAIN_ID'"
  [ -n "$BACKEND" ] || BACKEND=$(sed -n 's/^db_backend *= *"\(.*\)"/\1/p' "$CFG" | head -1)
  [ -f "$EXPECTED" ] || die "missing $EXPECTED"
  [ -n "$(expected_col "$MAXCK" 2 "$EXPECTED")" ] || die "$EXPECTED has no row for checkpoint $MAXCK"
  mkdir -p "$WORK"
  [ -f "$WORK/POISONED" ] && die "$(cat "$WORK/POISONED"): delete $HOME_DIR/data/application.db, $WORK/applied-to and $WORK/POISONED, then rerun"
fi
BACKEND=${BACKEND:-goleveldb}

# ---- 1. bundle integrity (manifest + detached signature, see tools/fastsync/manifest)
if [ "$SKIP_VERIFY" = 1 ]; then
  log "WARNING: --skip-bundle-verify: the bundle is not checked against a signed manifest (the applier still verifies every app hash)"
else
  [ -n "$MANIFEST" ] && [ -n "$PUB" ] || die "give --manifest and --pub (or --skip-bundle-verify)"
  if [ "$DRY_RUN" = 1 ]; then
    run "$MANIFEST_BIN" verify -dir "$CS_DIR" -manifest "$MANIFEST" -sig "$SIG" -pub "$PUB" -chain-id "$CHAIN_ID" -print-env
  else
    ENV_OUT=$("$MANIFEST_BIN" verify -dir "$CS_DIR" -manifest "$MANIFEST" -sig "$SIG" -pub "$PUB" -chain-id "$CHAIN_ID" -print-env) \
      || die "bundle verification failed"
    LAST_HEIGHT=$(sed -n 's/^LAST_HEIGHT=//p' <<< "$ENV_OUT")
    is_uint "$LAST_HEIGHT" || die "manifest verify printed no LAST_HEIGHT"
    [ "$MAXCK" -le "$LAST_HEIGHT" ] || die "checkpoint $MAXCK is above the bundle's last height $LAST_HEIGHT"
    log "bundle verified: $(tr '\n' ' ' <<< "$ENV_OUT")"
  fi
fi

# ---- 2. apply up to each checkpoint and publish it
CUR=$APPLIER
APPLIED=$(cat "$WORK/applied-to" 2>/dev/null || echo 0)
is_uint "$APPLIED" || die "bad $WORK/applied-to"

apply_to() { # $1 = target height; applies (APPLIED, target], every height verified against expected.tsv
  local tgt=$1 rc done_line lg=$WORK/apply-$1.log
  local args=(-db "$HOME_DIR/data" -backend "$BACKEND" -changesets "$CS_DIR" -expected "$EXPECTED" -require-trusted -to "$tgt")
  [ "$PARALLEL" = 1 ] && args+=(-parallel)
  [ "$tgt" -le "$APPLIED" ] && return 0   # e.g. a rerun after an upload failed
  if [ "$DRY_RUN" = 1 ]; then run "$CUR" apply "${args[@]}"; APPLIED=$tgt; return 0; fi
  while true; do
    log "apply to $tgt: $CUR apply ${args[*]}"
    set +e; "$CUR" apply "${args[@]}" > "$lg" 2>&1; rc=$?; set -e
    if [ "$rc" = 2 ]; then
      echo "APP HASH MISMATCH while applying to $tgt with $CUR ($(grep -m1 'APP HASH MISMATCH' "$lg" || true))" > "$WORK/POISONED"
      die "app hash mismatch during apply (see $lg); DB marked POISONED, nothing published for $tgt"
    fi
    [ "$rc" = 0 ] || { tail -5 "$lg" >&2; die "$CUR apply failed (rc=$rc), see $lg"; }
    if grep -q '^HANDOVER' "$lg" && [ "$CUR" = "$APPLIER" ]; then
      [ -n "$APPLIER50" ] || die "the bundle continues with app v4+ blocks: rerun with --applier50 PATH"
      CUR=$APPLIER50; log "handover to the v0.50 applier ($APPLIER50)"; continue
    fi
    break
  done
  done_line=$(grep '^done' "$lg" | tail -1)
  [ "$(sed -n 's/.*to height \([0-9]*\).*/\1/p' <<< "$done_line")" = "$tgt" ] || die "applier stopped at '$done_line', expected $tgt"
  grep -q 'recorded hash only: 0' <<< "$done_line" || die "some heights were not verified against trusted headers"
  APPLIED=$tgt; echo "$tgt" > "$WORK/applied-to"
}

publish() { # $1 = checkpoint height; the applier has exited, so application.db is closed and flushed
  local c=$1 d=$WORK/ckpt-$1 dst=$BUCKET/checkpoints/$1 tb=application.db.tar.zst app_hash next_hash sum bytes dbbytes
  if [ "$DRY_RUN" = 1 ]; then app_hash=DRYRUN next_hash=DRYRUN; else
    app_hash=$(expected_col "$c" 2 "$EXPECTED") next_hash=$(expected_col "$c" 3 "$EXPECTED")
    [ -n "$app_hash" ] || die "no expected app hash for $c"
  fi
  run rm -rf "$d"; run mkdir -p "$d"
  local t0=$SECONDS
  run_sh "tar -C $(printf %q "$HOME_DIR/data") -cf - application.db | zstd -q -T0 -3 -o $(printf %q "$d/$tb")"
  run_sh "cd $(printf %q "$d") && sha256sum $tb > $tb.sha256"
  if [ "$DRY_RUN" = 1 ]; then sum=DRYRUN bytes=0 dbbytes=0; else
    sum=$(cut -d' ' -f1 "$d/$tb.sha256") bytes=$(stat -c %s "$d/$tb") dbbytes=$(du -sb "$HOME_DIR/data/application.db" | cut -f1)
    cat > "$d/CHECKPOINT" <<EOF
format=1
chain_id=$CHAIN_ID
height=$c
app_hash_after=$app_hash
next_header_hash=$next_hash
tarball=$tb
tarball_sha256=$sum
tarball_bytes=$bytes
app_db_bytes=$dbbytes
db_backend=$BACKEND
applier=$(basename "$CUR")
store_format=$(if [ "$CUR" = "$APPLIER" ]; then echo v046; else echo v050; fi)
created_at=$(date -u +%FT%TZ)
EOF
  fi
  gcs cp "$d/$tb" "$dst/$tb"
  gcs cp "$d/$tb.sha256" "$dst/$tb.sha256"
  gcs cp "$d/CHECKPOINT" "$dst/CHECKPOINT"   # last: marks the checkpoint complete
  run rm -rf "$d"
  log "checkpoint $c published to $dst (app hash $app_hash, $bytes bytes, $((SECONDS - t0))s)"
}

MISSING=()
for c in "${CKS[@]}"; do
  if gcs_exists "$BUCKET/checkpoints/$c/CHECKPOINT"; then
    log "checkpoint $c already published"
    continue
  fi
  if [ "$APPLIED" -gt "$c" ]; then
    log "WARNING: checkpoint $c is not published but the DB is already at $APPLIED; cannot produce it from this DB"
    MISSING+=("$c"); continue
  fi
  apply_to "$c"
  publish "$c"
  [ "$DRY_RUN" = 1 ] || { printf '%s applied=%s published=%s\n' "$(date -u +%FT%TZ)" "$APPLIED" "$c" >> "$WORK/progress.log"
    gcs cp "$WORK/progress.log" "$BUCKET/progress/apply-checkpoints.log" >/dev/null 2>&1 || true; }
done
[ "${#MISSING[@]}" = 0 ] || die "checkpoints not produced (DB was past them): ${MISSING[*]}"
log "DONE: ${#CKS[@]} checkpoint(s) up to $MAXCK published under $BUCKET/checkpoints/"
