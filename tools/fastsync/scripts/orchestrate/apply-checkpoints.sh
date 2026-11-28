#!/usr/bin/env bash
# apply-checkpoints.sh: apply VM of the multi-VM "fully verified" mode. Replays the changeset bundle into
# application.db and, every N blocks, publishes a checkpoint (app DB tarball + CHECKPOINT with height, app hash
# and tarball sha256) to a bucket, from which segment VMs (segment-run.sh) start their re-execution.
#
#   apply-checkpoints.sh --home DIR --changesets DIR --expected expected.tsv --chain-id ID --bucket gs://B/prefix
#                        --to H [--every 250000] [--work DIR] [--no-parallel] [--dry-run]
#                        (--manifest manifest.json --pub bundle.pub [--sig manifest.json.sig] | --skip-bundle-verify)
#   env: APPLIER, APPLIER50 (the appliers, as for ../fastsync.sh), MANIFEST_BIN (default manifest), GCLOUD
#
# --home      an initialized node home (genesis + config.toml); <home>/data/application.db is the applier's target.
#             It may hold a partial apply: <work>/applied-to records the last verified height.
# --expected  fshdr output verified for --chain-id, covering 1..H. Every applied height is checked against it
#             (-require-trusted), so a checkpoint is only published after its app hash matched the verified chain.
# Checkpoints are the multiples of --every up to --to. One is complete once its CHECKPOINT object exists
# (uploaded after the tarball); published checkpoints are skipped on a rerun.
#
# The bundle is made read-only (chmod a-w) before `manifest verify`, so it cannot be changed between the
# verification and the apply by anyone but its owner or root. Run as a dedicated non-root user that owns it.
# The apply loop is the one fastsync.sh uses (../lib-apply.sh): exit status 2 of the applier means an app hash
# mismatch, and the DB is then marked POISONED and never resumed or published. fastsync.sh --checkpoints is not
# used because it keeps homes on local disk and returns only after the last height; here each checkpoint is
# published as soon as it is applied, so segment VMs start while the apply runs.
set -euo pipefail
LOG_TAG=apply-checkpoints
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "$0")/lib.sh"

HOME_DIR='' CS_DIR='' EXPECTED='' CHAIN_ID='' BUCKET='' EVERY=250000 TO='' WORK=''
MANIFEST='' SIG='' PUB='' SKIP_VERIFY=0 PARALLEL=1
APPLIER=${APPLIER:-} APPLIER50=${APPLIER50:-} MANIFEST_BIN=${MANIFEST_BIN:-manifest}
usage() { sed -n 6,9p "$0" >&2; exit 2; }
while [ $# -gt 0 ]; do
  case $1 in
    --home) HOME_DIR=$2; shift ;;
    --changesets) CS_DIR=$2; shift ;;
    --expected) EXPECTED=$2; shift ;;
    --chain-id) CHAIN_ID=$2; shift ;;
    --bucket) BUCKET=${2%/}; shift ;;
    --every) EVERY=$2; shift ;;
    --to) TO=$2; shift ;;
    --work) WORK=$2; shift ;;
    --manifest) MANIFEST=$2; shift ;;
    --sig) SIG=$2; shift ;;
    --pub) PUB=$2; shift ;;
    --skip-bundle-verify) SKIP_VERIFY=1 ;;
    --no-parallel) PARALLEL=0 ;;
    --dry-run) DRY_RUN=1 ;;
    -h | --help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
  shift
done
[ -n "$HOME_DIR" ] && [ -n "$CS_DIR" ] && [ -n "$EXPECTED" ] && [ -n "$CHAIN_ID" ] && [ -n "$BUCKET" ] && [ -n "$TO" ] || usage
check_bucket "$BUCKET"
check_chain_id "$CHAIN_ID"
need_uint --every "$EVERY"; need_uint --to "$TO"
[ "$EVERY" -ge 1 ] && [ "$TO" -ge "$EVERY" ] || die "need 1 <= --every <= --to"
[ $((TO / EVERY)) -le 100000 ] || die "too many checkpoints: raise --every"
[ -n "$APPLIER" ] || die "set APPLIER (and APPLIER50 for app v4+ blocks)"
[ "$SKIP_VERIFY" = 1 ] || { [ -n "$MANIFEST" ] && [ -n "$PUB" ]; } || die "give --manifest and --pub (or --skip-bundle-verify)"
WORK=${WORK:-$HOME_DIR/fastsync-orch}
SIG=${SIG:-$MANIFEST.sig}
mapfile -t CKS < <(seq "$EVERY" "$EVERY" "$TO")
MAXCK=${CKS[${#CKS[@]}-1]}
log "chain=$CHAIN_ID checkpoints=${#CKS[@]} (${CKS[0]}..$MAXCK) bucket=$BUCKET"

# ---- preflight (skipped in dry runs, whose inputs need not exist)
BACKEND=goleveldb
if [ "$DRY_RUN" != 1 ]; then
  need_tools tar zstd sha256sum awk "${GCLOUD_CMD[0]}" "$APPLIER"
  CFG=$HOME_DIR/config/config.toml GEN=$HOME_DIR/config/genesis.json
  [ -f "$CFG" ] && [ -f "$GEN" ] || die "need $CFG and $GEN"
  GEN_CHAIN=$(grep -o -m1 '"chain_id": *"[^"]*"' "$GEN" | head -1 | sed 's/.*"\([^"]*\)"$/\1/' || true)
  [ "$GEN_CHAIN" = "$CHAIN_ID" ] || die "genesis chain_id '$GEN_CHAIN' != --chain-id '$CHAIN_ID'"
  B=$(sed -n 's/^db_backend *= *"\(.*\)"/\1/p' "$CFG" | head -1); BACKEND=${B:-goleveldb}
  [ -f "$EXPECTED" ] || die "missing $EXPECTED"
  check_expected "$EXPECTED" "$CHAIN_ID"
  [ -n "$(expected_col "$MAXCK" 2 "$EXPECTED")" ] || die "$EXPECTED has no row for checkpoint $MAXCK"
  mkdir -p "$WORK"
  [ ! -f "$WORK/POISONED" ] || die "$(cat "$WORK/POISONED"); delete $HOME_DIR/data/application.db, $WORK/applied-to and $WORK/POISONED, then rerun"
fi

# ---- 1. bundle integrity: freeze it, then check it against the signed manifest (cmd/manifest)
if [ "$SKIP_VERIFY" = 1 ]; then
  log "WARNING: --skip-bundle-verify: no signed manifest check (the applier still checks every app hash)"
else
  run chmod -R a-w "$CS_DIR" || die "cannot make $CS_DIR read-only: run as the user that owns it"
  if [ "$DRY_RUN" = 1 ]; then
    run "$MANIFEST_BIN" verify -dir "$CS_DIR" -manifest "$MANIFEST" -sig "$SIG" -pub "$PUB" -chain-id "$CHAIN_ID" -print-env
  else
    ENV_OUT=$("$MANIFEST_BIN" verify -dir "$CS_DIR" -manifest "$MANIFEST" -sig "$SIG" -pub "$PUB" -chain-id "$CHAIN_ID" -print-env) ||
      die "bundle verification failed"
    LAST_HEIGHT=$(sed -n 's/^LAST_HEIGHT=//p' <<< "$ENV_OUT")
    is_uint "$LAST_HEIGHT" && [ "$MAXCK" -le "$LAST_HEIGHT" ] || die "checkpoint $MAXCK is above the bundle's last height '$LAST_HEIGHT'"
    log "bundle verified: $(tr '\n' ' ' <<< "$ENV_OUT")"
  fi
fi

# ---- 2. apply up to each checkpoint and publish it
apply_init

publish() { # $1 = checkpoint height; the applier has exited, so application.db is closed and flushed
  local c=$1 d=$WORK/ckpt-$1 dst=$BUCKET/checkpoints/$1 tb=application.db.tar.zst app_hash='' sum='' format
  if [ "$DRY_RUN" != 1 ]; then
    app_hash=$(expected_col "$c" 2 "$EXPECTED")
    [ -n "$app_hash" ] || die "no expected app hash for $c"
  fi
  if [ "$CUR" = "$APPLIER" ]; then format=v046; else format=v050; fi
  run rm -rf "$d"
  run mkdir -p "$d"
  run_sh "tar -C $(printf %q "$HOME_DIR/data") -cf - application.db | zstd -q -T0 -3 -o $(printf %q "$d/$tb")"
  if [ "$DRY_RUN" != 1 ]; then
    sum=$(sha256sum < "$d/$tb" | cut -d' ' -f1)
    printf 'chain_id=%s\nheight=%s\napp_hash_after=%s\ntarball=%s\ntarball_sha256=%s\nstore_format=%s\n' \
      "$CHAIN_ID" "$c" "$app_hash" "$tb" "$sum" "$format" > "$d/CHECKPOINT"
  fi
  gcs cp "$d/$tb" "$dst/$tb"
  gcs cp "$d/CHECKPOINT" "$dst/CHECKPOINT" # last: marks the checkpoint complete
  run rm -rf "$d"
  log "checkpoint $c published to $dst (app hash $app_hash)"
}

MISSING=()
for c in "${CKS[@]}"; do
  if gcs_exists "$BUCKET/checkpoints/$c/CHECKPOINT"; then log "checkpoint $c already published"; continue; fi
  if [ "$APPLIED" -gt "$c" ]; then # resume: application.db is already past c
    log "skip checkpoint $c: application.db is already at $APPLIED"; MISSING+=("$c"); continue
  fi
  apply_to "$c"
  publish "$c"
done
[ "${#MISSING[@]}" = 0 ] || die "checkpoints not produced (the DB was past them): ${MISSING[*]}"
log "DONE: ${#CKS[@]} checkpoint(s) up to $MAXCK published under $BUCKET/checkpoints/"
