#!/usr/bin/env bash
# segment-run.sh: segment VM of the multi-VM "fully verified" mode. Turns checkpoint C (published by
# apply-checkpoints.sh) into a node home, runs ../segment.sh on it to fully re-execute C+1..END and check the
# boundary against the verified header chain, and uploads a PASS/FAIL result to the bucket.
#
#   segment-run.sh --bucket gs://B/prefix --checkpoint C|genesis --end END --home DIR --expected expected.tsv
#                  --chain-id ID --peer ID@HOST:PORT (--rpc URL[,URL...] | --boot-src-data DIR)
#                  [--port-prefix 26] [--inter-block-cache sync-only|off|keep] [--max-requesters N]
#                  [--fastindex] [--env "K=V ..."] [--start-flags "..."] [--work DIR] [--dry-run]
#
# --home      a fresh, initialized node home (genesis, config.toml, app.toml; for a multiplexer build the
#             embedded child binaries under <home>/bin); data/ must not hold application/state/blockstore DBs.
# --expected  fshdr output covering C..END (row h: app hash after h, hash of header h+1). It is the trust
#             anchor: the checkpoint's metadata and sha256 come from the bucket and only protect transport.
# --peer      the persistent peer segment.sh gives the node (it serves blocks C+1..END+1).
# Speed settings written to the home before the run (no environment knobs):
#   --inter-block-cache  app.toml [performance] inter-block-cache (default sync-only: the node serves no queries);
#                        "keep" leaves app.toml alone. Binaries without the setting ignore the key.
#   --max-requesters     config.toml [blocksync] max_requesters (default: unset = the node's default).
#   --fastindex          build the IAVL fast-node index offline (fastindex / fastindex46, by the checkpoint's store
#                        format) instead of letting the node rebuild it silently at its first start.
#
# Steps: 1. download the checkpoint, check sha256, height, chain id and app hash (== expected.tsv row C);
#        2. extract application.db, optional fastindex, `fsboot bootstrap` the CometBFT stores at C;
#        3. ../segment.sh HOME END PORT_PREFIX PEER EXPECTED: `celestia-appd start --halt-height END+1`, clean halt,
#           and the node's own headers END-19..END equal expected.tsv.
# Exit status: 0 PASS, 2 FAIL (result uploaded), 1 usage or setup error.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
LOG_TAG=segment-run
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$HERE/lib.sh"

BUCKET='' CK='' END='' HOME_DIR='' EXPECTED='' CHAIN_ID='' RPC='' SRC_DATA='' PEER='' PORT_PREFIX=26
IBC=sync-only MAXREQ='' FASTINDEX=0 EXTRA_ENV='' START_FLAGS='' WORK=''
APPD=${APPD:-celestia-appd} FSBOOT=${FSBOOT:-fsboot} FSHDR=${FSHDR:-fshdr} SEGMENT_SH=${SEGMENT_SH:-$HERE/../segment.sh}
FASTINDEX46=${FASTINDEX46:-fastindex46} FASTINDEX50=${FASTINDEX50:-fastindex}
usage() { sed -n 6,9p "$0" >&2; exit 1; }
while [ $# -gt 0 ]; do
  case $1 in
    --bucket) BUCKET=${2%/}; shift;; --checkpoint) CK=$2; shift;; --end) END=$2; shift;; --home) HOME_DIR=$2; shift;;
    --expected) EXPECTED=$2; shift;; --chain-id) CHAIN_ID=$2; shift;; --rpc) RPC=$2; shift;;
    --boot-src-data) SRC_DATA=$2; shift;; --peer) PEER=$2; shift;; --port-prefix) PORT_PREFIX=$2; shift;;
    --inter-block-cache) IBC=$2; shift;; --max-requesters) MAXREQ=$2; shift;; --fastindex) FASTINDEX=1;;
    --env) EXTRA_ENV=$2; shift;; --start-flags) START_FLAGS=$2; shift;; --work) WORK=$2; shift;;
    --appd) APPD=$2; shift;; --fsboot) FSBOOT=$2; shift;; --fshdr) FSHDR=$2; shift;; --segment-sh) SEGMENT_SH=$2; shift;;
    --dry-run) DRY_RUN=1;; -h|--help) usage;;
    *) echo "unknown arg $1" >&2; usage;;
  esac
  shift
done
[ -n "$BUCKET" ] && [ -n "$CK" ] && [ -n "$END" ] && [ -n "$HOME_DIR" ] && [ -n "$EXPECTED" ] && [ -n "$CHAIN_ID" ] && [ -n "$PEER" ] || usage
check_bucket "$BUCKET"
[[ $CHAIN_ID =~ ^[A-Za-z0-9._-]{1,64}$ ]] || die "bad --chain-id '$CHAIN_ID'"
[[ $PEER =~ ^[0-9a-fA-F]{40}@[A-Za-z0-9.:_-]+:[0-9]{1,5}$ ]] || die "--peer must be NODEID@HOST:PORT"
need_uint --end "$END"; need_uint --port-prefix "$PORT_PREFIX"
case $IBC in sync-only|off|keep) ;; *) die "--inter-block-cache must be sync-only, off or keep";; esac
[ -z "$MAXREQ" ] || { need_uint --max-requesters "$MAXREQ"; [ "$MAXREQ" -ge 2 ] && [ "$MAXREQ" -le 1000 ] || die "--max-requesters must be in [2, 1000]"; }
if [ "$CK" = genesis ]; then C=0; else need_uint --checkpoint "$CK"; C=$CK; [ "$C" -ge 1 ] || die "use --checkpoint genesis for height 0"; fi
[ "$END" -ge $((C + 20)) ] || die "--end ($END) must be at least checkpoint + 20 (segment.sh checks 20 boundary rows)"
[ "$C" = 0 ] || [ -n "$RPC" ] || [ -n "$SRC_DATA" ] || die "give --rpc or --boot-src-data (validator sets and commit for fsboot)"
read -r -a ENV_KV <<< "$EXTRA_ENV"
for kv in "${ENV_KV[@]}"; do [[ $kv =~ ^[A-Z_][A-Z0-9_]*=[A-Za-z0-9._:/-]*$ ]] || die "bad --env entry '$kv' (want KEY=VALUE)"; done
read -r -a START_ARGS <<< "$START_FLAGS"
WORK=${WORK:-$HOME_DIR/segment}
RES_PREFIX=$BUCKET/results/$((C + 1))-$END

# ---- result reporting: every exit after this point uploads RESULT (FAIL unless the run passed)
STATUS=FAIL REASON="setup failed" T_RUN=0
report() {
  local rc=$?
  [ "$STATUS" = PASS ] || [ "$rc" != 0 ] || rc=2
  if [ "$DRY_RUN" = 1 ]; then log "dry run: would upload RESULT status=$STATUS to $RES_PREFIX/"; exit "$rc"; fi
  mkdir -p "$WORK"
  { echo "status=$STATUS"; echo "reason=$REASON"; echo "chain_id=$CHAIN_ID"; echo "checkpoint=$CK"
    echo "first_height=$((C + 1))"; echo "end_height=$END"; echo "blocks=$((END - C))"; echo "run_seconds=$T_RUN"
    echo "host=$(hostname)"; echo "finished_at=$(date -u +%FT%TZ)"; } > "$WORK/RESULT"
  [ -f "$HOME_DIR/segment.log" ] && tail -200 "$HOME_DIR/segment.log" > "$WORK/segment.log.tail"
  [ -f "$WORK/segment.log.tail" ] && { gcs cp "$WORK/segment.log.tail" "$RES_PREFIX/segment.log.tail" >/dev/null 2>&1 || true; }
  gcs cp "$WORK/RESULT" "$RES_PREFIX/RESULT" >/dev/null 2>&1 || log "WARNING: could not upload $RES_PREFIX/RESULT"
  log "SEGMENT $((C + 1))..$END: $STATUS ($REASON)"
  exit "$rc"
}
trap report EXIT
fail() { REASON=$1; echo "[$LOG_TAG] FAIL: $1" >&2; exit 2; }

log "segment $((C + 1))..$END from checkpoint $CK, result $RES_PREFIX/"

# ---- 0. preflight
if [ "$DRY_RUN" != 1 ]; then
  need_tools awk sha256sum tar zstd "${GCLOUD_CMD[0]}" "$APPD" "$FSHDR" "$FSBOOT" "$SEGMENT_SH"
  [ -f "$HOME_DIR/config/config.toml" ] && [ -f "$HOME_DIR/config/genesis.json" ] && [ -f "$HOME_DIR/config/app.toml" ] ||
    fail "$HOME_DIR is not an initialized node home"
  for d in application.db state.db blockstore.db; do
    [ -e "$HOME_DIR/data/$d" ] && fail "$HOME_DIR/data/$d exists: use a fresh home"
  done
  [ -f "$EXPECTED" ] || fail "missing $EXPECTED"
  [ -n "$(expected_col "$END" 3 "$EXPECTED")" ] || fail "$EXPECTED has no row $END"
  [ "$C" = 0 ] || [ -n "$(expected_col "$C" 2 "$EXPECTED")" ] || fail "$EXPECTED has no row for checkpoint $C"
  mkdir -p "$WORK"
fi
BACKEND=$(sed -n 's/^db_backend *= *"\(.*\)"/\1/p' "$HOME_DIR/config/config.toml" 2>/dev/null | head -1 || true)
BACKEND=${BACKEND:-goleveldb}

# ---- 1-2. checkpoint -> application.db (+ fast-node index) + CometBFT stores at C
FORMAT=''
if [ "$C" != 0 ]; then
  SRC=$BUCKET/checkpoints/$C TB=application.db.tar.zst
  REASON="checkpoint download failed"
  gcs cp "$SRC/CHECKPOINT" "$SRC/$TB.sha256" "$SRC/$TB" "$WORK/"
  if [ "$DRY_RUN" != 1 ]; then
    ckv() { sed -n "s/^$1=//p" "$WORK/CHECKPOINT" | head -1; }
    [ "$(ckv height)" = "$C" ] || fail "CHECKPOINT height '$(ckv height)' != $C"
    [ "$(ckv chain_id)" = "$CHAIN_ID" ] || fail "CHECKPOINT chain_id '$(ckv chain_id)' != $CHAIN_ID"
    WANT=$(expected_col "$C" 2 "$EXPECTED")
    [ "${WANT^^}" = "$(ckv app_hash_after | tr '[:lower:]' '[:upper:]')" ] || fail "checkpoint app hash $(ckv app_hash_after) != verified $WANT"
    [ "$(cut -d' ' -f1 "$WORK/$TB.sha256")" = "$(ckv tarball_sha256)" ] || fail "sha256 file and CHECKPOINT disagree"
    (cd "$WORK" && sha256sum -c --status "$TB.sha256") || fail "checkpoint tarball sha256 mismatch"
    FORMAT=$(ckv store_format)
    log "checkpoint $C verified (sha256, chain id, app hash $WANT)"
  fi
  REASON="checkpoint extract failed"
  run_sh "zstd -q -dc $(printf %q "$WORK/$TB") | tar -C $(printf %q "$HOME_DIR/data") -xf - application.db"
  run rm -f "$WORK/$TB"
  if [ "$FASTINDEX" = 1 ]; then
    REASON="fastindex failed"
    if [ "$FORMAT" = v046 ]; then FI=$FASTINDEX46; else FI=$FASTINDEX50; fi
    run "$FI" -db "$HOME_DIR/data" -backend "$BACKEND"
  fi
  REASON="fsboot bootstrap failed"
  BOOT=(bootstrap -home "$HOME_DIR" -height "$C" -expected "$EXPECTED")
  if [ -n "$SRC_DATA" ]; then BOOT+=(-src-data "$SRC_DATA"); else BOOT+=(-rpc "$RPC"); fi
  run "$FSBOOT" "${BOOT[@]}"
fi
# the checkpoint marker segment.sh reads (same format as fastsync.sh checkpoint homes)
run_sh "printf 'height\\t%s\\n' $C > $(printf %q "$HOME_DIR/CHECKPOINT")"

# ---- speed settings in the home's config files
REASON="config update failed"
[ "$IBC" = keep ] || run set_toml "$HOME_DIR/config/app.toml" performance inter-block-cache "\"$IBC\""
[ -z "$MAXREQ" ] || run set_toml "$HOME_DIR/config/config.toml" blocksync max_requesters "$MAXREQ"

# ---- 3. full re-execution and boundary check (segment.sh)
REASON="segment.sh failed"
CMD=(env "${ENV_KV[@]}" APPD="$APPD" FSHDR="$FSHDR" "$SEGMENT_SH" "$HOME_DIR" "$END" "$PORT_PREFIX" "$PEER" "$EXPECTED" "${START_ARGS[@]}")
T0=$SECONDS
if [ "$DRY_RUN" = 1 ]; then run "${CMD[@]}"; else
  log "run: ${CMD[*]}"
  set +e; "${CMD[@]}" > "$WORK/segment.out" 2>&1; rc=$?; set -e
  T_RUN=$((SECONDS - T0))
  cat "$WORK/segment.out" >&2
  LAST=$(grep '^SEGMENT ' "$WORK/segment.out" | tail -1 || true)
  [ "$rc" = 0 ] || fail "${LAST:-segment.sh exited $rc}"
  [[ $LAST == *": OK "* ]] || fail "segment.sh exited 0 without an OK line"
  REASON=$LAST
fi
STATUS=PASS
[ "$DRY_RUN" = 1 ] && REASON="dry run"
true
