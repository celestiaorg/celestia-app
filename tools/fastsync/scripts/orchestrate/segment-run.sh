#!/usr/bin/env bash
# segment-run.sh: segment VM of the multi-VM "fully verified" mode. Turns checkpoint C (published by
# apply-checkpoints.sh) into a node home, runs ../segment.sh on it to fully re-execute C+1..END and check the
# boundary against the verified header chain, and uploads a PASS/FAIL result to the bucket.
#
#   segment-run.sh --bucket gs://B/prefix --checkpoint C|genesis --end END --home DIR --expected expected.tsv
#                  --chain-id ID --peer NODEID@HOST:PORT (--rpc URL,URL[,...] [--allow-single-source] | --boot-src-data DIR)
#                  [--port-prefix 26] [--inter-block-cache sync-only|off|keep] [--max-requesters N] [--fastindex]
#                  [--max-db-bytes N] [--work DIR] [--dry-run]
#   env: APPD, FSBOOT, FSHDR, FASTINDEX46, FASTINDEX50, SEGMENT_SH (default ../segment.sh), GCLOUD
#
# --home      a fresh, initialized node home (genesis, config.toml, app.toml; for a multiplexer build the embedded
#             child binaries under <home>/bin); data/ must not hold application, state or block store DBs.
# --expected  fshdr output verified for --chain-id, covering C..END. It is the only trust anchor: the bucket
#             (checkpoint tarball and CHECKPOINT) is untrusted. What binds the downloaded application.db to the
#             verified chain is fsboot, which writes the state at C with the AppHash of the verified header C+1,
#             and the CometBFT handshake at start, which refuses an app DB whose app hash differs. The tarball's
#             sha256 and the CHECKPOINT fields are only transport and consistency checks.
# The tarball is untrusted input: it is decompressed with a size cap (--max-db-bytes, default 1 TiB), only
# application.db is extracted, without owners or modes from the archive, and anything but regular files and
# directories in it fails the segment. Run as a dedicated non-root user.
# Speed settings written to the home: --inter-block-cache (app.toml [performance] inter-block-cache, default
# sync-only; "keep" leaves app.toml alone), --max-requesters (config.toml [blocksync] max_requesters), --fastindex
# (build the IAVL fast-node index offline, by the checkpoint's store_format).
# Exit status: 0 PASS, 2 FAIL (result uploaded), 1 usage or setup error.
set -euo pipefail
LOG_TAG=segment-run
HERE=$(cd "$(dirname "$0")" && pwd)
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$HERE/lib.sh"

BUCKET='' CK='' END='' HOME_DIR='' EXPECTED='' CHAIN_ID='' RPC='' SRC_DATA='' SINGLE_SOURCE=0 PEER='' PORT_PREFIX=26
IBC=sync-only MAXREQ='' FASTINDEX=0 MAX_DB=$((1 << 40)) WORK=''
APPD=${APPD:-celestia-appd} FSBOOT=${FSBOOT:-fsboot} FSHDR=${FSHDR:-fshdr} SEGMENT_SH=${SEGMENT_SH:-$HERE/../segment.sh}
FASTINDEX46=${FASTINDEX46:-fastindex46} FASTINDEX50=${FASTINDEX50:-fastindex}
usage() { sed -n 6,10p "$0" >&2; exit 1; }
while [ $# -gt 0 ]; do
  case $1 in
    --bucket) BUCKET=${2%/}; shift ;;
    --checkpoint) CK=$2; shift ;;
    --end) END=$2; shift ;;
    --home) HOME_DIR=$2; shift ;;
    --expected) EXPECTED=$2; shift ;;
    --chain-id) CHAIN_ID=$2; shift ;;
    --rpc) RPC=$2; shift ;;
    --allow-single-source) SINGLE_SOURCE=1 ;;
    --boot-src-data) SRC_DATA=$2; shift ;;
    --peer) PEER=$2; shift ;;
    --port-prefix) PORT_PREFIX=$2; shift ;;
    --inter-block-cache) IBC=$2; shift ;;
    --max-requesters) MAXREQ=$2; shift ;;
    --fastindex) FASTINDEX=1 ;;
    --max-db-bytes) MAX_DB=$2; shift ;;
    --work) WORK=$2; shift ;;
    --dry-run) DRY_RUN=1 ;;
    -h | --help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
  shift
done
[ -n "$BUCKET" ] && [ -n "$CK" ] && [ -n "$END" ] && [ -n "$HOME_DIR" ] && [ -n "$EXPECTED" ] && [ -n "$CHAIN_ID" ] && [ -n "$PEER" ] || usage
check_bucket "$BUCKET"
check_chain_id "$CHAIN_ID"
[[ $PEER =~ ^[0-9a-fA-F]{40}@[A-Za-z0-9.:_-]+:[0-9]{1,5}$ ]] || die "--peer must be NODEID@HOST:PORT"
need_uint --end "$END"; need_uint --port-prefix "$PORT_PREFIX"; need_uint --max-db-bytes "$MAX_DB"
case $IBC in sync-only | off | keep) ;; *) die "--inter-block-cache must be sync-only, off or keep" ;; esac
if [ -n "$MAXREQ" ]; then
  need_uint --max-requesters "$MAXREQ"
  [ "$MAXREQ" -ge 2 ] && [ "$MAXREQ" -le 1000 ] || die "--max-requesters must be in [2, 1000]"
fi
if [ "$CK" = genesis ]; then C=0; else need_uint --checkpoint "$CK"; C=$CK; [ "$C" -ge 1 ] || die "use --checkpoint genesis for height 0"; fi
[ "$END" -ge $((C + 20)) ] || die "--end ($END) must be at least checkpoint + 20 (segment.sh checks 20 boundary rows)"
if [ "$C" != 0 ] && [ -z "$SRC_DATA" ]; then
  [ -n "$RPC" ] || die "give --rpc or --boot-src-data (validator sets and commit for fsboot)"
  n=$(tr ',' '\n' <<< "$RPC" | sed 's#/*$##' | grep . | sort -u | wc -l)
  [ "$n" -ge 2 ] || [ "$SINGLE_SOURCE" = 1 ] ||
    die "fsboot needs two distinct --rpc servers: one alone is trusted for proposer priorities and unhashed params (--allow-single-source accepts that)"
fi
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
    echo "first_height=$((C + 1))"; echo "end_height=$END"; echo "run_seconds=$T_RUN"
    echo "host=$(hostname)"; echo "finished_at=$(date -u +%FT%TZ)"; } > "$WORK/RESULT"
  if [ -f "$HOME_DIR/segment.log" ]; then
    tail -200 "$HOME_DIR/segment.log" > "$WORK/segment.log.tail"
    gcs cp "$WORK/segment.log.tail" "$RES_PREFIX/segment.log.tail" >/dev/null 2>&1 || true
  fi
  gcs cp "$WORK/RESULT" "$RES_PREFIX/RESULT" >/dev/null 2>&1 || log "WARNING: could not upload $RES_PREFIX/RESULT"
  log "SEGMENT $((C + 1))..$END: $STATUS ($REASON)"
  exit "$rc"
}
trap report EXIT
fail() { REASON=$1; echo "[$LOG_TAG] FAIL: $1" >&2; exit 2; }

log "segment $((C + 1))..$END from checkpoint $CK, result $RES_PREFIX/"

# ---- 0. preflight
if [ "$DRY_RUN" != 1 ]; then
  need_tools awk sha256sum tar zstd head find "${GCLOUD_CMD[0]}" "$APPD" "$FSHDR" "$FSBOOT" "$SEGMENT_SH"
  [ -f "$HOME_DIR/config/config.toml" ] && [ -f "$HOME_DIR/config/genesis.json" ] && [ -f "$HOME_DIR/config/app.toml" ] ||
    fail "$HOME_DIR is not an initialized node home"
  for d in application.db state.db blockstore.db; do
    [ -e "$HOME_DIR/data/$d" ] && fail "$HOME_DIR/data/$d exists: use a fresh home"
  done
  [ -f "$EXPECTED" ] || fail "missing $EXPECTED"
  (check_expected "$EXPECTED" "$CHAIN_ID") || fail "$EXPECTED is not fshdr output verified for $CHAIN_ID"
  [ -n "$(expected_col "$END" 3 "$EXPECTED")" ] || fail "$EXPECTED has no row $END"
  [ "$C" = 0 ] || [ -n "$(expected_col "$C" 2 "$EXPECTED")" ] || fail "$EXPECTED has no row for checkpoint $C"
  mkdir -p "$WORK"
fi
BACKEND=$(sed -n 's/^db_backend *= *"\(.*\)"/\1/p' "$HOME_DIR/config/config.toml" 2>/dev/null | head -1 || true)
BACKEND=${BACKEND:-goleveldb}

# ---- 1-2. checkpoint -> application.db (+ fast-node index) + CometBFT stores at C
FORMAT=v050
if [ "$C" != 0 ]; then
  SRC=$BUCKET/checkpoints/$C TB=$WORK/application.db.tar.zst DATA=$HOME_DIR/data
  REASON="checkpoint download failed"
  gcs cp "$SRC/CHECKPOINT" "$SRC/application.db.tar.zst" "$WORK/"
  if [ "$DRY_RUN" != 1 ]; then
    ckv() { sed -n "s/^$1=//p" "$WORK/CHECKPOINT" | head -1; }
    [ "$(ckv height)" = "$C" ] || fail "CHECKPOINT height '$(ckv height)' != $C"
    [ "$(ckv chain_id)" = "$CHAIN_ID" ] || fail "CHECKPOINT chain_id '$(ckv chain_id)' != $CHAIN_ID"
    # consistency only (fail early): the binding to the verified chain is fsboot + the handshake, see above
    WANT=$(expected_col "$C" 2 "$EXPECTED")
    [ "${WANT^^}" = "$(ckv app_hash_after | tr '[:lower:]' '[:upper:]')" ] || fail "CHECKPOINT app hash $(ckv app_hash_after) != expected.tsv $WANT"
    [ "$(sha256sum < "$TB" | cut -d' ' -f1)" = "$(ckv tarball_sha256)" ] || fail "checkpoint tarball sha256 mismatch"
    FORMAT=$(ckv store_format)
  fi
  REASON="checkpoint extract failed"
  # size cap first (an archive cut at the cap could otherwise extract cleanly), then a capped, restricted extract
  if [ "$DRY_RUN" != 1 ]; then
    n=$(zstd -q -dc "$TB" | head -c $((MAX_DB + 1)) | wc -c) || fail "cannot decompress the checkpoint tarball"
    [ "$n" -le "$MAX_DB" ] || fail "checkpoint tarball decompresses to more than --max-db-bytes $MAX_DB"
  fi
  run_sh "zstd -q -dc $(printf %q "$TB") | head -c $MAX_DB | tar -C $(printf %q "$DATA") --no-same-owner --no-same-permissions -xf - application.db"
  if [ "$DRY_RUN" != 1 ]; then
    bad=$(find "$DATA/application.db" ! -type f ! -type d -print -quit)
    [ -z "$bad" ] || fail "checkpoint tarball holds a non-regular entry: ${bad#"$DATA"/}"
    others=$(find "$DATA" -mindepth 1 -maxdepth 1 ! -name application.db -print -quit)
    [ -z "$others" ] || fail "unexpected entry after extraction: $others"
  fi
  run rm -f "$TB"
  if [ "$FASTINDEX" = 1 ]; then
    REASON="fastindex failed"
    if [ "$FORMAT" = v046 ]; then FI=$FASTINDEX46; else FI=$FASTINDEX50; fi
    run "$FI" -db "$DATA" -backend "$BACKEND"
  fi
  REASON="fsboot bootstrap failed"
  BOOT=(bootstrap -home "$HOME_DIR" -height "$C" -expected "$EXPECTED")
  if [ -n "$SRC_DATA" ]; then BOOT+=(-src-data "$SRC_DATA"); else BOOT+=(-rpc "$RPC"); fi
  [ "$SINGLE_SOURCE" = 1 ] && BOOT+=(-allow-single-source)
  run "$FSBOOT" "${BOOT[@]}"
fi
# the checkpoint marker segment.sh reads (the format of fastsync.sh checkpoint homes)
run_sh "printf 'height\\t%s\\n' $C > $(printf %q "$HOME_DIR/CHECKPOINT")"

# ---- speed settings in the home's config files
REASON="config update failed"
[ "$IBC" = keep ] || run set_toml "$HOME_DIR/config/app.toml" performance inter-block-cache "\"$IBC\""
[ -z "$MAXREQ" ] || run set_toml "$HOME_DIR/config/config.toml" blocksync max_requesters "$MAXREQ"

# ---- 3. full re-execution and boundary check
REASON="segment.sh failed"
CMD=(env APPD="$APPD" FSHDR="$FSHDR" "$SEGMENT_SH" "$HOME_DIR" "$END" "$PORT_PREFIX" "$PEER" "$EXPECTED")
if [ "$DRY_RUN" = 1 ]; then
  run "${CMD[@]}"
  REASON="dry run"
else
  T0=$SECONDS
  set +e
  "${CMD[@]}" > "$WORK/segment.out" 2>&1
  rc=$?
  set -e
  T_RUN=$((SECONDS - T0))
  cat "$WORK/segment.out"
  LAST=$(grep '^SEGMENT ' "$WORK/segment.out" | tail -1 || true)
  [ "$rc" = 0 ] || fail "${LAST:-segment.sh exited $rc}"
  [[ $LAST == *": OK "* ]] || fail "segment.sh exited 0 without an OK line"
  REASON=$LAST
fi
STATUS=PASS
