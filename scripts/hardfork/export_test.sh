#!/usr/bin/env bash
set -euo pipefail

# Run export.sh against stub celestia-appd, curl and GNU time.
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SCRIPT=$SCRIPT_DIR/export.sh
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

if command -v shellcheck >/dev/null 2>&1; then
    shellcheck "$SCRIPT_DIR"/*.sh
fi

STUBS=$TMP/stubs
mkdir -p "$STUBS"
export STUB_LOG=$TMP/calls.log STUB_STATE=$TMP/state PATH=$STUBS:$PATH

# The node answers on the local RPC while STUB_STATE/running exists. Files in
# STUB_STATE set the app height (app-height), the app hash in base64
# (app-hash), the exported initial height and chain-id (initial, chain), the
# halt log lines (halt), a leftover embedded app (orphan), a binary that
# ignores the signer override (ignore-env), an upgrade at height 21 (window),
# next_validators_hash of block 21 (next-vals) and the presence of
# "genesis from-export" (from-export).
cat >"$STUBS/celestia-appd" <<'EOF'
#!/usr/bin/env bash
echo "celestia-appd $*" >>"$STUB_LOG"
state() { cat "$STUB_STATE/$1" 2>/dev/null || echo "$2"; }
case "$1 $2" in
"version ") echo 11.0.0 ;;
"init export-signer")
    mkdir -p "$4/config" "$4/data"
    echo '{"pub_key":{"value":"dGhyb3c="}}' >"$4/config/priv_validator_key.json"
    ;;
"comet show-validator")
    key=${CELESTIA_APPD_PRIV_VALIDATOR_KEY_FILE:-$4/config/priv_validator_key.json}
    if [ -f "$STUB_STATE/ignore-env" ]; then key=$4/config/priv_validator_key.json; fi
    echo "{\"@type\":\"/cosmos.crypto.ed25519.PubKey\",\"key\":\"$(jq -r .pub_key.value "$key")\"}" >&2
    ;;
"start "*)
    echo "signer $CELESTIA_APPD_PRIV_VALIDATOR_KEY_FILE $CELESTIA_APPD_PRIV_VALIDATOR_STATE_FILE $CELESTIA_APPD_NODE_KEY_FILE" >>"$STUB_LOG"
    if [ -f "$STUB_STATE/orphan" ]; then
        bash -c 'sleep 30; :' appd start --home="$3" >/dev/null 2>&1 &
        echo $! >"$STUB_STATE/orphan.pid"
    fi
    if [ -f "$STUB_STATE/halt" ]; then
        cat "$STUB_STATE/halt"
        exit 2
    fi
    touch "$STUB_STATE/running"
    trap 'rm -f "$STUB_STATE/running"; exit 0' TERM
    while :; do sleep 1; done
    ;;
"export "*)
    if [ -f "$STUB_STATE/window" ] && [ "$8" != --allow-upgrade-window ]; then
        echo 'upgrade "v12" is scheduled for height 21: export with --height 19 instead' >&2
        exit 1
    fi
    printf '{\n  "chain_id": "%s",\n  "initial_height": %s,\n  "app_state": {}\n}\n' "$(state chain test)" "$(state initial 21)" >"$7"
    ;;
"genesis --help")
    echo "Available Commands:"
    if [ -f "$STUB_STATE/from-export" ]; then echo "  from-export  Build a genesis from an export"; fi
    ;;
"genesis from-export")
    while [ "$1" != --output ]; do shift; done
    echo '{"chain_id":"test"}' >"$2"
    ;;
esac
EOF
cat >"$STUBS/curl" <<'EOF'
#!/usr/bin/env bash
for url; do :; done
state() { cat "$STUB_STATE/$1" 2>/dev/null || echo "$2"; }
case "$url" in
http://localhost:26657/*)
    [ -f "$STUB_STATE/running" ] || exit 7
    echo "{\"result\":{\"response\":{\"last_block_height\":\"$(state app-height 20)\",\"last_block_app_hash\":\"$(state app-hash q80S)\"}}}"
    ;;
*/block\?height=*)
    height=${url##*=}
    hash=0000
    if [ "$height" = 21 ]; then hash=ABCD12; fi
    echo "{\"result\":{\"block\":{\"header\":{\"height\":\"$height\",\"time\":\"2026-11-16T10:00:00.123456789Z\",\"chain_id\":\"test\",\"app_hash\":\"$hash\",\"validators_hash\":\"AA\",\"next_validators_hash\":\"$(state next-vals AA)\"}}}}"
    ;;
*) exit 7 ;;
esac
EOF
# GNU time -v -o FILE, as time and gtime.
cat >"$STUBS/time" <<'EOF'
#!/usr/bin/env bash
[ "$1" = -v ] || exit 1
out=/dev/null
if [ "$2" = -o ]; then out=$3; shift 2; fi
shift
status=0
"$@" || status=$?
printf '\tMaximum resident set size (kbytes): 4242\n' >"$out"
exit "$status"
EOF
cp "$STUBS/time" "$STUBS/gtime"
if ! command -v sha256sum >/dev/null 2>&1; then
    cat >"$STUBS/sha256sum" <<'EOF'
#!/usr/bin/env bash
exec shasum -a 256 "$@"
EOF
fi
chmod +x "$STUBS"/*

# new_node creates a stopped node home with a validator key and an empty
# output path.
new_node() {
    rm -rf "${TMP:?}/home" "${TMP:?}/out" "$STUB_STATE" "$STUB_LOG"
    NODE_HOME=$TMP/home
    OUT=$TMP/out
    mkdir -p "$NODE_HOME/config" "$NODE_HOME/data" "$STUB_STATE"
    echo '{"pub_key":{"value":"cmVhbA=="}}' >"$NODE_HOME/config/priv_validator_key.json"
    echo '{"height":"20","round":0,"step":3}' >"$NODE_HOME/data/priv_validator_state.json"
    touch "$STUB_LOG"
}

export_sh() {
    bash "$SCRIPT" --home "$NODE_HOME" --out "$OUT" --rpc http://ref:26657 --height 20 "$@" >"$TMP/out.log" 2>&1
}

fail() {
    echo "FAIL: $*" >&2
    cat "$TMP/out.log" >&2
    exit 1
}

called() {
    grep -q "^celestia-appd $1" "$STUB_LOG"
}

summary() {
    grep -qxF "$1" "$OUT/summary.txt" || fail "summary line: $1"
}

# happy path with from-export: a throwaway signer, sync to the height, a
# matching app hash, export, genesis, timings and summary.
new_node
touch "$STUB_STATE/from-export"
export_sh --genesis-time 2026-11-17T00:00:00Z --initial-height 40 --chain-id test-2 || fail "export with genesis failed"
called "init export-signer --home $OUT/signer --recover=false" || fail "init flags"
called "start --home $NODE_HOME --halt-height 21 --halt-time 0 --priv_validator_laddr " || fail "start flags"
grep -qxF "signer $OUT/signer/config/priv_validator_key.json $OUT/signer/data/priv_validator_state.json $OUT/signer/config/node_key.json" "$STUB_LOG" ||
    fail "node not started with the throwaway signer"
grep -q '"20"' "$NODE_HOME/data/priv_validator_state.json" || fail "real signer state changed"
called "export --home $NODE_HOME --height 20 --output-document $OUT/export.json" || fail "export command"
SHA=$(sha256sum "$OUT/export.json" | awk '{ print $1 }')
called "genesis from-export $OUT/export.json --export-sha256 $SHA --export-height 20 --export-block-time 2026-11-16T10:00:00.123456789Z --genesis-time 2026-11-17T00:00:00Z --initial-height 40 --chain-id test-2 --output $OUT/genesis.json" ||
    fail "from-export command"
[ ! -f "$STUB_STATE/running" ] || fail "node left running"
(cd "$OUT" && sha256sum -c export.json.sha256 >/dev/null && sha256sum -c genesis.json.sha256 >/dev/null) || fail "sha256 files"
printf 'height: 20\nchain_id: test\nexport_sha256: %s\napp_hash_at_height: matches block 21: abcd12\nupgrade_window: not needed\n' "$SHA" |
    diff - "$OUT/export.manifest" || fail "manifest"
[ "$(jq -r .app_hash_after_height "$OUT/header.json")" = abcd12 ] || fail "header.json app hash"
summary "app_hash_at_height: matches block 21: abcd12"
summary "export_peak_rss_kb: 4242"
summary "genesis_peak_rss_kb: 4242"
awk -F'\t' '$1 == "export_state" && $6 == 4242 { found = 1 } END { exit !found }' "$OUT/timings.tsv" ||
    fail "export timings row"
[ "$(wc -l <"$OUT/timings.tsv")" -eq 8 ] || fail "timings.tsv rows"

# resume: no sync, export or from-export again.
: >"$STUB_LOG"
export_sh --genesis-time 2026-11-17T00:00:00Z --initial-height 40 --chain-id test-2 || fail "resume failed"
! { called start || called export || called "genesis from-export"; } || fail "resume ran a step again"
grep -q "reusing export.json $SHA" "$TMP/out.log" || fail "export not reused"
grep -q "reusing genesis.json" "$TMP/out.log" || fail "genesis not reused"
summary "app_hash_at_height: matches block 21: abcd12"
summary "export_peak_rss_kb: 4242"

# resume with other genesis inputs: refused.
! export_sh --genesis-time 2026-11-17T00:00:00Z --initial-height 41 --chain-id test-2 || fail "resume with other genesis inputs accepted"
grep -q "genesis.json was built from other inputs" "$TMP/out.log" || fail "genesis inputs message"

# resume for another height: refused before header.json is written.
cp "$OUT/header.json" "$TMP/header.json"
! bash "$SCRIPT" --home "$NODE_HOME" --out "$OUT" --rpc http://ref:26657 --height 19 >"$TMP/out.log" 2>&1 ||
    fail "resume for another height accepted"
grep -q "export.manifest is not for height 19 on test" "$TMP/out.log" || fail "manifest mismatch message"
cmp -s "$TMP/header.json" "$OUT/header.json" || fail "rejected resume rewrote header.json"

# halt in block sync, no from-export, no GNU time: the app hash was checked by
# block sync, the genesis command is printed.
new_node
printf '%s\n' 'ERR error in proxyAppConn.FinalizeBlock err="halt per configuration height 21 time 0" module=state' \
    'panic: Failed to process committed block (21:AB): halt per configuration height 21 time 0' >"$STUB_STATE/halt"
printf '#!/usr/bin/env bash\nexit 1\n' | tee "$STUBS/time" >"$STUBS/gtime"
export_sh --genesis-time 2026-11-17T00:00:00Z --initial-height 40 || fail "export at a block sync halt failed"
summary "app_hash_at_height: checked by block sync against block 21"
summary "export_peak_rss_kb: n/a"
grep -q "^genesis_sha256: skipped; run: .*genesis from-export .*--initial-height 40 --output" "$OUT/summary.txt" ||
    fail "skipped genesis command"
[ ! -e "$OUT/genesis.json" ] || fail "genesis built without from-export"

# halt in handshake replay, from-export without --genesis-time: the app hash is
# not verified, the command has a placeholder.
new_node
echo "error during handshake: halt per configuration height 21 time 0" >"$STUB_STATE/halt"
touch "$STUB_STATE/from-export"
export_sh --initial-height 40 || fail "export at a handshake halt failed"
summary "app_hash_at_height: not verified (halt path)"
grep -q "^genesis_sha256: skipped; run: .*--genesis-time GENESIS_TIME" "$OUT/summary.txt" || fail "genesis time placeholder"

# upgrade at height 21 and block 20 changed no validators: a second export
# with --allow-upgrade-window.
new_node
touch "$STUB_STATE/window"
export_sh || fail "export in the upgrade window failed"
[ "$(grep -c "^celestia-appd export " "$STUB_LOG")" = 2 ] || fail "export not retried once"
called "export --home $NODE_HOME --height 20 --output-document $OUT/export.json --allow-upgrade-window" ||
    fail "retry without --allow-upgrade-window"
summary "upgrade_window: allowed (header 21 validators_hash == next_validators_hash)"

# refusals: nothing is marked as exported and no node is left running.
for c in running signer app-hash initial-height chain-id window timeout orphan not-empty; do
    new_node
    args=()
    case "$c" in
    running) touch "$STUB_STATE/running"; want="stop it first" ;;
    signer) touch "$STUB_STATE/ignore-env"; want="does not take the signer from CELESTIA_APPD_PRIV_VALIDATOR_KEY_FILE" ;;
    app-hash) echo AAAA >"$STUB_STATE/app-hash"; want="the local state differs from the chain" ;;
    initial-height) echo 20 >"$STUB_STATE/initial"; want="export.json has initial_height 20, expected 21" ;;
    chain-id) echo other >"$STUB_STATE/chain"; want="export.json has chain_id other, expected test" ;;
    window) touch "$STUB_STATE/window"; echo BB >"$STUB_STATE/next-vals"; want="block 20 changed validators or header 21 is unknown" ;;
    timeout) echo 19 >"$STUB_STATE/app-height"; args=(--sync-timeout 3); want="did not reach height 20 (app at 19)" ;;
    orphan) touch "$STUB_STATE/orphan"; want="process for $NODE_HOME runs (pid" ;;
    not-empty) mkdir -p "$OUT"; touch "$OUT/node.log"; want="is not empty" ;;
    esac
    if export_sh ${args[@]+"${args[@]}"}; then
        fail "accepted: $c"
    fi
    grep -qF -- "$want" "$TMP/out.log" || fail "$c message"
    [ ! -e "$OUT/export.json.sha256" ] || fail "$c marked an export as done"
    case "$c" in
    running) ;;
    orphan) kill "$(cat "$STUB_STATE/orphan.pid")" ;;
    *) [ ! -f "$STUB_STATE/running" ] || fail "$c left the node running" ;;
    esac
    case "$c" in running | signer | not-empty) ! called start || fail "$c started the node" ;; esac
done

echo "export script checks passed"
