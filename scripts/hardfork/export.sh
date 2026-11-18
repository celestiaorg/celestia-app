#!/usr/bin/env bash
# Export the app state of a stopped node at a height, and optionally build a
# candidate genesis from it.
# Run "export.sh --help" for usage.

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: export.sh --height H --rpc URL --out DIR [flags]

Syncs a stopped node to height H, exports its state at H, and optionally
builds a candidate genesis from the export.

Flags:
  --height H             last good height to export (required); it must be on
                         the app version this binary runs natively
  --rpc URL              reference RPC that has block H (required)
  --out DIR              output directory, absent or empty (required)
  --home DIR             node home (default ~/.celestia-app); restore a
                         snapshot into it by hand first if needed
  --binary PATH          celestia-appd (default: on PATH)
  --local-rpc URL        RPC of the node this script starts
                         (default http://localhost:26657)
  --sync-timeout SEC     how long to wait for height H (default 7200)

Candidate genesis, built with "celestia-appd genesis from-export". Without
--genesis-time and --initial-height, or without the command, the step is
skipped and the command to run later is printed:
  --genesis-time RFC3339
  --initial-height N
  --chain-id ID
  --accept-mint-excess X

The node must be stopped. The script starts it with a throwaway signer key
from --out, so it never signs, and with --halt-height H+1 --halt-time 0,
which refuses block H+1, so the app stops at H. Then it stops the node.

--out gets export.json, export.json.sha256, export.manifest, header.json,
node.log, timings.tsv and summary.txt, and genesis.json, genesis.json.sha256
and genesis.json.report.json when the genesis is built. A re-run with the
same --out, --height and chain reuses a matching export.json and
genesis.json. Otherwise remove --out first.
EOF
}

HEIGHT=""
RPC=""
OUT=""
NODE_HOME=$HOME/.celestia-app
BIN=""
LOCAL_RPC=http://localhost:26657
SYNC_TIMEOUT=7200
GENESIS_TIME=""
INITIAL_HEIGHT=""
CHAIN_ID=""
MINT_EXCESS=""
RESUME=0
NODE_PID=""
APP_HASH_CHECK=""
GENESIS_SHA=""

die() {
    echo "error: $*" >&2
    exit 1
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
        -h | --help) usage; exit 0 ;;
        esac
        [ $# -ge 2 ] || die "$1 needs a value"
        case "$1" in
        --height) HEIGHT=$2 ;;
        --rpc) RPC=${2%/} ;;
        --out) OUT=$2 ;;
        --home) NODE_HOME=${2%/} ;;
        --binary) BIN=$2 ;;
        --local-rpc) LOCAL_RPC=${2%/} ;;
        --sync-timeout) SYNC_TIMEOUT=$2 ;;
        --genesis-time) GENESIS_TIME=$2 ;;
        --initial-height) INITIAL_HEIGHT=$2 ;;
        --chain-id) CHAIN_ID=$2 ;;
        --accept-mint-excess) MINT_EXCESS=$2 ;;
        *) die "unknown flag: $1" ;;
        esac
        shift 2
    done
}

check_args() {
    [[ "$HEIGHT" =~ ^[1-9][0-9]*$ ]] || die "--height must be a positive number"
    [ -n "$RPC" ] || die "--rpc is required"
    [ -n "$OUT" ] || die "--out is required"
    [[ "$SYNC_TIMEOUT" =~ ^[1-9][0-9]*$ ]] || die "--sync-timeout must be a positive number"
    if [ -n "$INITIAL_HEIGHT" ] && ! [[ "$INITIAL_HEIGHT" =~ ^[1-9][0-9]*$ ]]; then
        die "--initial-height must be a positive number"
    fi
    BIN=$(command -v "${BIN:-celestia-appd}") || die "celestia-appd not found; use --binary"
    [ -d "$NODE_HOME/config" ] || die "no node home at $NODE_HOME"
    NODE_HOME=$(cd "$NODE_HOME" && pwd)
    NEXT=$((HEIGHT + 1))
    # GNU time reports the peak RSS of a command; on macOS it is gtime.
    TIME_BIN=$(type -P gtime || type -P time || true)
    if [ -n "$TIME_BIN" ] && ! "$TIME_BIN" -v true >/dev/null 2>&1; then
        TIME_BIN=""
    fi
}

sha256() {
    { sha256sum "$1" 2>/dev/null || shasum -a 256 "$1"; } | awk '{ print $1 }'
}

# setup_out creates OUT. An OUT with export.json.sha256 is a re-run, checked
# by check_resume.
setup_out() {
    mkdir -p "$OUT"
    OUT=$(cd "$OUT" && pwd)
    if [ -f "$OUT/export.json.sha256" ]; then
        RESUME=1
    elif [ -n "$(ls -A "$OUT")" ]; then
        die "$OUT is not empty"
    fi
}

# step runs a step and appends its wall time, the sizes of data/ and OUT and
# the free space in OUT at the end of the step, and the peak RSS set by
# measured to timings.tsv.
step() {
    local start=$SECONDS data out free
    STEP_RSS=n/a
    echo "== $1"
    "$1"
    data=$(du -sk "$NODE_HOME/data" 2>/dev/null | awk '{ print $1 }' || true)
    out=$(du -sk "$OUT" | awk '{ print $1 }')
    free=$(df -Pk "$OUT" | awk 'NR == 2 { print $4 }')
    if [ ! -f "$OUT/timings.tsv" ]; then
        printf 'step\tseconds\tdata_kb_end\tout_kb_end\tout_free_kb_end\tpeak_rss_kb\n' >"$OUT/timings.tsv"
    fi
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$1" $((SECONDS - start)) "${data:-0}" "$out" "$free" "$STEP_RSS" >>"$OUT/timings.tsv"
}

# measured runs a command with its output in LOG, under GNU time if there is
# one, and sets STEP_RSS to its peak RSS in KiB.
measured() {
    local log=$1 status=0
    shift
    if [ -z "$TIME_BIN" ]; then
        "$@" >"$log" 2>&1 || status=$?
        return "$status"
    fi
    "$TIME_BIN" -v -o "$log.time" "$@" >"$log" 2>&1 || status=$?
    STEP_RSS=$(awk -F': ' '/Maximum resident set size/ { print $2 }' "$log.time")
    STEP_RSS=${STEP_RSS:-n/a}
    return "$status"
}

# node_processes lists the processes started for NODE_HOME, including embedded
# apps of the multiplexer, which get --home=NODE_HOME. It fails if ps fails.
node_processes() {
    local list
    list=$(ps axww -o pid=,command=) || return 1
    H=$NODE_HOME awk '{ l = $0 " " } index(l, " start ") && (index(l, "--home=" ENVIRON["H"] " ") || index(l, "--home " ENVIRON["H"] " ")) { print $1 }' <<<"$list"
}

# check_no_node dies if a process for NODE_HOME runs.
check_no_node() {
    local procs
    procs=$(node_processes) || die "cannot list processes"
    [ -z "$procs" ] || die "a celestia-appd process for $NODE_HOME runs (pid $procs); stop it first"
}

preflight() {
    local dir free
    VERSION=$("$BIN" version 2>&1) || die "cannot run $BIN version"
    echo "binary $BIN, version $VERSION"
    check_no_node
    if curl -s -m 5 -o /dev/null "$LOCAL_RPC/status"; then
        die "a node answers at $LOCAL_RPC; stop it first"
    fi
    for dir in "$NODE_HOME" "$OUT"; do
        free=$(df -Pk "$dir" | awk 'NR == 2 { print $4 }')
        echo "$free KiB free in $dir"
        [ "$free" -ge $((1024 * 1024)) ] || die "less than 1 GiB free in $dir"
    done
}

# rpc_block prints block $1 from RPC, or fails if RPC does not have it.
rpc_block() {
    local body
    body=$(curl -sf -m 30 "$RPC/block?height=$1") || return 1
    [ "$(jq -r '.result.block.header.height // empty' <<<"$body" 2>/dev/null)" = "$1" ] || return 1
    echo "$body"
}

# reference_header reads the time and chain-id of block HEIGHT, and the app
# hash after HEIGHT from block HEIGHT+1 if it exists.
reference_header() {
    local block
    block=$(rpc_block "$HEIGHT") || die "$RPC has no block $HEIGHT"
    BLOCK_TIME=$(jq -r .result.block.header.time <<<"$block")
    CHAIN=$(jq -r .result.block.header.chain_id <<<"$block")
    REF_HASH=""
    if block=$(rpc_block "$NEXT"); then
        REF_HASH=$(jq -r .result.block.header.app_hash <<<"$block" | tr '[:upper:]' '[:lower:]')
    else
        echo "warning: $RPC has no block $NEXT; the app hash after $HEIGHT is not known"
    fi
    if [ "$RESUME" = 1 ]; then
        check_resume
    fi
    jq -n --arg rpc "$RPC" --arg h "$HEIGHT" --arg t "$BLOCK_TIME" --arg c "$CHAIN" --arg a "$REF_HASH" \
        '{rpc: $rpc, height: $h, time: $t, chain_id: $c, app_hash_after_height: (if $a == "" then null else $a end)}' \
        >"$OUT/header.json"
    echo "block $HEIGHT: time $BLOCK_TIME, chain-id $CHAIN, app hash after it ${REF_HASH:-unknown}"
}

manifest() {
    printf 'height: %s\nchain_id: %s\nexport_sha256: %s\n' "$HEIGHT" "$CHAIN" "$EXPORT_SHA"
}

# check_resume reuses export.json from an earlier run only if it matches its
# sha256 and its manifest matches this run.
check_resume() {
    EXPORT_SHA=$(sha256 "$OUT/export.json") || die "cannot read $OUT/export.json"
    [ "$EXPORT_SHA" = "$(awk '{ print $1 }' "$OUT/export.json.sha256")" ] ||
        die "$OUT/export.json does not match export.json.sha256; remove $OUT"
    [ "$(manifest)" = "$(head -n 3 "$OUT/export.manifest" 2>/dev/null)" ] ||
        die "$OUT/export.manifest is not for height $HEIGHT on $CHAIN; remove $OUT"
    APP_HASH_CHECK=$(sed -n 's/^app_hash_at_height: //p' "$OUT/export.manifest")
    echo "reusing export.json $EXPORT_SHA"
}

# setup_signer creates a throwaway signer in OUT/signer and checks that the
# binary uses its key, read from the key file, for NODE_HOME. The SDK reads
# config overrides from <EXECUTABLE NAME>_<KEY> environment variables.
setup_signer() {
    local signer=$OUT/signer prefix want out
    # --recover=false and no stdin: an inherited <prefix>_RECOVER must not
    # rebuild a key from a mnemonic.
    out=$("$BIN" init export-signer --home "$signer" --recover=false </dev/null 2>&1) ||
        die "cannot create a throwaway signer: $out"
    want=$(jq -r '.pub_key.value // empty' "$signer/config/priv_validator_key.json" 2>/dev/null || true)
    prefix=$(basename "$(readlink -f "$BIN" 2>/dev/null || echo "$BIN")" | tr '[:lower:]' '[:upper:]' | tr '.-' '__')
    SIGNER_ENV=(env "${prefix}_PRIV_VALIDATOR_KEY_FILE=$signer/config/priv_validator_key.json"
        "${prefix}_PRIV_VALIDATOR_STATE_FILE=$signer/data/priv_validator_state.json"
        "${prefix}_NODE_KEY_FILE=$signer/config/node_key.json")
    # show-validator prints to stderr.
    out=$("${SIGNER_ENV[@]}" "$BIN" comet show-validator --home "$NODE_HOME" 2>&1 || true)
    if [ -z "$want" ] || [ "$(sed -n 's/.*"key":"\([^"]*\)".*/\1/p' <<<"$out")" != "$want" ]; then
        die "$BIN does not take the signer from ${prefix}_PRIV_VALIDATOR_KEY_FILE; not starting the node"
    fi
    echo "throwaway signer $want"
}

# local_app sets APP_HEIGHT and APP_HASH (hex) from the local RPC, or fails.
local_app() {
    local body
    body=$(curl -sf -m 5 "$LOCAL_RPC/abci_info") || return 1
    APP_HEIGHT=$(jq -r '.result.response.last_block_height // "0"' <<<"$body")
    [[ "$APP_HEIGHT" =~ ^[0-9]+$ ]] || return 1
    APP_HASH=$(jq -r '.result.response.last_block_app_hash // ""' <<<"$body" | base64 -d | od -An -v -tx1 | tr -d ' \n')
}

stop_node() {
    [ -n "$NODE_PID" ] || return 0
    if kill -0 "$NODE_PID" 2>/dev/null; then
        kill -TERM "$NODE_PID" 2>/dev/null || true
        for _ in $(seq 60); do
            kill -0 "$NODE_PID" 2>/dev/null || break
            sleep 1
        done
        kill -KILL "$NODE_PID" 2>/dev/null || true
    fi
    wait "$NODE_PID" 2>/dev/null || true
    NODE_PID=""
}

# sync_node starts the node with the throwaway signer, no remote signer and
# --halt-height HEIGHT+1, waits until the app is at HEIGHT and stops it. Block
# sync applies HEIGHT only once it has HEIGHT+1, so the node usually exits on
# the halt before the RPC shows HEIGHT.
sync_node() {
    local deadline=$((SECONDS + SYNC_TIMEOUT)) reached="" halt="halt per configuration height $NEXT time 0"
    APP_HEIGHT=0
    "${SIGNER_ENV[@]}" "$BIN" start --home "$NODE_HOME" --halt-height "$NEXT" --halt-time 0 \
        --priv_validator_laddr "" >"$OUT/node.log" 2>&1 &
    NODE_PID=$!
    while [ "$SECONDS" -lt "$deadline" ]; do
        if local_app && [ "$APP_HEIGHT" -ge "$HEIGHT" ]; then
            reached=rpc
            break
        fi
        if ! kill -0 "$NODE_PID" 2>/dev/null; then
            if grep -qF "$halt" "$OUT/node.log"; then
                reached=halt
            fi
            break
        fi
        sleep 2
    done
    stop_node
    check_no_node
    case "$reached" in
    rpc) check_app_hash ;;
    halt)
        # The app hash the node computed for HEIGHT, from the "finalized block"
        # line in node.log (log level info, plain or JSON format).
        APP_HEIGHT=$HEIGHT
        APP_HASH=$(sed $'s/\033\\[[0-9;]*m//g' "$OUT/node.log" | grep -F "finalized block" |
            grep -E "(^|[ {,])\"?height\"?[=:]\"?$HEIGHT([ ,}\"]|\$)" | tail -n 1 |
            sed -nE 's/.*block_app_hash"?[=:]"?([0-9A-Fa-f]+).*/\1/p' | tr '[:upper:]' '[:lower:]' || true)
        if [ -n "$APP_HASH" ]; then
            check_app_hash
        else
            APP_HASH_CHECK="not verified (node.log has no finalized block $HEIGHT)"
            echo "the node stopped at the halt; app hash at $HEIGHT $APP_HASH_CHECK"
        fi
        ;;
    *) die "the node did not reach height $HEIGHT (app at $APP_HEIGHT); see $OUT/node.log" ;;
    esac
}

# check_app_hash compares the local app hash at HEIGHT with block HEIGHT+1.
check_app_hash() {
    if [ "$APP_HEIGHT" -gt "$HEIGHT" ]; then
        echo "warning: the app is at $APP_HEIGHT; the export needs height $HEIGHT not to be pruned"
        APP_HASH_CHECK="not verified (the app is at $APP_HEIGHT)"
    elif [ -z "$REF_HASH" ]; then
        APP_HASH_CHECK="not verified ($RPC has no block $NEXT)"
    elif [ "$APP_HASH" = "$REF_HASH" ]; then
        APP_HASH_CHECK="matches block $NEXT: $REF_HASH"
    else
        die "app hash at $HEIGHT is $APP_HASH, block $NEXT has $REF_HASH: the local state differs from the chain"
    fi
    echo "app at $APP_HEIGHT, app hash $APP_HASH_CHECK"
}

# export_state exports the state at HEIGHT and checks its initial height and
# chain-id. A refused export shows the end of its log, which says what to do,
# e.g. export a lower height when an upgrade is due.
export_state() {
    local top initial chain
    measured "$OUT/export.log" "$BIN" export --home "$NODE_HOME" --height "$HEIGHT" --output-document "$OUT/export.json" ||
        die "export failed, see $OUT/export.log:"$'\n'"$(tail -n 5 "$OUT/export.log")"
    # The outer fields come before app_state; the file can be gigabytes.
    top=$(head -c 65536 "$OUT/export.json")
    initial=$(grep -m1 -Eo '"initial_height"[[:space:]]*:[[:space:]]*"?[0-9]+' <<<"$top" | grep -Eo '[0-9]+$' || true)
    [ "$initial" = "$NEXT" ] || die "export.json has initial_height ${initial:-?}, expected $NEXT"
    chain=$(grep -m1 -Eo '"chain_id"[[:space:]]*:[[:space:]]*"[^"]*"' <<<"$top" | sed 's/.*:[[:space:]]*"//; s/"$//' || true)
    [ "$chain" = "$CHAIN" ] || die "export.json has chain_id ${chain:-?}, expected $CHAIN"
}

# hash_export writes the manifest, then the sha256 file that marks the export
# as done.
hash_export() {
    EXPORT_SHA=$(sha256 "$OUT/export.json")
    { manifest; echo "app_hash_at_height: $APP_HASH_CHECK"; } >"$OUT/export.manifest"
    printf '%s  export.json\n' "$EXPORT_SHA" >"$OUT/export.json.sha256.tmp"
    mv "$OUT/export.json.sha256.tmp" "$OUT/export.json.sha256"
    echo "export.json sha256 $EXPORT_SHA"
}

# build_genesis runs "genesis from-export", or prints the command if the tool
# or its inputs are missing. A matching genesis.json from an earlier run is
# kept.
build_genesis() {
    local help cmd inputs
    cmd=(genesis from-export "$OUT/export.json" --export-sha256 "$EXPORT_SHA" --export-height "$HEIGHT"
        --export-block-time "$BLOCK_TIME" --genesis-time "${GENESIS_TIME:-GENESIS_TIME}"
        --initial-height "${INITIAL_HEIGHT:-INITIAL_HEIGHT}")
    [ -z "$CHAIN_ID" ] || cmd+=(--chain-id "$CHAIN_ID")
    [ -z "$MINT_EXCESS" ] || cmd+=(--accept-mint-excess "$MINT_EXCESS")
    cmd+=(--output "$OUT/genesis.json")
    inputs=$(printf '%q ' "${cmd[@]}")
    if [ -f "$OUT/genesis.json.sha256" ]; then
        [ "$(cat "$OUT/genesis.inputs" 2>/dev/null)" = "$inputs" ] ||
            die "$OUT/genesis.json was built from other inputs; remove it, genesis.json.sha256 and genesis.json.report.json to rebuild"
        GENESIS_SHA=$(awk '{ print $1 }' "$OUT/genesis.json.sha256")
        [ "$(sha256 "$OUT/genesis.json")" = "$GENESIS_SHA" ] || die "$OUT/genesis.json does not match genesis.json.sha256"
        echo "reusing genesis.json $GENESIS_SHA"
        return 0
    fi
    # "genesis from-export --help" succeeds even without the command, so look
    # for it in the list of genesis commands.
    help=$("$BIN" genesis --help 2>&1 || true)
    if [ -z "$GENESIS_TIME" ] || [ -z "$INITIAL_HEIGHT" ] || ! grep -Eq '^[[:space:]]+from-export[[:space:]]' <<<"$help"; then
        GENESIS_SHA="skipped; run: $(printf '%q ' "$BIN" "${cmd[@]}")"
        echo "genesis $GENESIS_SHA"
        return 0
    fi
    measured "$OUT/genesis.log" "$BIN" "${cmd[@]}" || die "genesis from-export failed; see $OUT/genesis.log"
    GENESIS_SHA=$(sha256 "$OUT/genesis.json")
    echo "$inputs" >"$OUT/genesis.inputs"
    printf '%s  genesis.json\n' "$GENESIS_SHA" >"$OUT/genesis.json.sha256"
    echo "genesis.json sha256 $GENESIS_SHA, report $OUT/genesis.json.report.json"
    # TODO: compare the supply and escrow totals of the report with RPC queries at HEIGHT.
}

# write_summary writes summary.txt; sizes and peak RSS come from timings.tsv.
write_summary() {
    {
        printf 'height: %s\nblock_time: %s\nchain_id: %s\nbinary: %s %s\n' "$HEIGHT" "$BLOCK_TIME" "$CHAIN" "$BIN" "$VERSION"
        printf 'app_hash_at_height: %s\nexport_sha256: %s\ngenesis_sha256: %s\n' "$APP_HASH_CHECK" "$EXPORT_SHA" "$GENESIS_SHA"
        awk -F'\t' 'NR > 1 {
            if ($3 + 0 > d) d = $3
            if ($4 + 0 > o) o = $4
            if ($6 != "n/a") rss[$1] = $6
        } END {
            printf "max_data_kb_at_step_end: %d\nmax_out_kb_at_step_end: %d\n", d, o
            printf "export_peak_rss_kb: %s\n", ("export_state" in rss) ? rss["export_state"] : "n/a"
            printf "genesis_peak_rss_kb: %s\n", ("build_genesis" in rss) ? rss["build_genesis"] : "n/a"
        }' "$OUT/timings.tsv"
        echo "total_seconds: $SECONDS"
    } >"$OUT/summary.txt"
    cat "$OUT/summary.txt"
}

main() {
    parse_args "$@"
    check_args
    echo "export.sh sha256 $(sha256 "${BASH_SOURCE[0]}")"
    setup_out
    trap stop_node EXIT
    trap 'exit 143' TERM; trap 'exit 130' INT
    step preflight
    step reference_header
    if [ "$RESUME" = 0 ]; then
        step setup_signer
        step sync_node
        step export_state
        step hash_export
    fi
    step build_genesis
    write_summary
}

main "$@"
