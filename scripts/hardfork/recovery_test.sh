#!/usr/bin/env bash
set -euo pipefail

# Run recovery.sh against stub celestia-appd, sudo, systemctl, docker and curl.
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SCRIPT=$SCRIPT_DIR/recovery.sh
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
# The cases expect sudo and the root guard, so run as a regular user.
[ "$(id -u)" != 0 ] || { echo "run as a non-root user" >&2; exit 1; }

if command -v shellcheck >/dev/null 2>&1; then
    shellcheck "$SCRIPT_DIR"/*.sh
fi

STUBS=$TMP/stubs
mkdir -p "$STUBS"
export STUB_LOG=$TMP/calls.log STUB_STATE=$TMP/state PATH=$STUBS:$PATH
unset DAEMON_HOME

# Every stub appends its argv to STUB_LOG.
cat >"$STUBS/celestia-appd" <<'EOF'
#!/usr/bin/env bash
echo "$0 $*" >>"$STUB_LOG"
case "$*" in
version) echo 10.0.0 ;;
*--dry-run*) ;;
"snapshots list"*) printf 'height: 17 format: 3 chunks: 1\nheight: 19 format: 3 chunks: 1\n' >&2 ;;
rollback*)
    if [ -f "$STUB_STATE/fail-once" ]; then
        rm "$STUB_STATE/fail-once"
        exit 1
    fi
    if [ -f "$STUB_STATE/edit-signer" ]; then
        echo '{"height":"21","round":0,"step":3}' >"$3/data/priv_validator_state.json"
    fi
    if [ -f "$STUB_STATE/delete-signer" ]; then
        rm "$3/data/priv_validator_state.json"
    fi
    echo "Rolled back to height $5"
    ;;
esac
EOF
cat >"$STUBS/sudo" <<'EOF'
#!/usr/bin/env bash
echo "sudo $*" >>"$STUB_LOG"
exec "$@"
EOF
cat >"$STUBS/systemctl" <<'EOF'
#!/usr/bin/env bash
echo "systemctl $*" >>"$STUB_LOG"
case "$1" in
stop)
    rm -f "$STUB_STATE/active"
    if [ -f "$STUB_STATE/sign-on-stop" ]; then
        echo '{"height":"31","round":0,"step":3}' >"$(cat "$STUB_STATE/sign-on-stop")"
    fi
    ;;
start) touch "$STUB_STATE/active" ;;
is-active) [ -f "$STUB_STATE/active" ] ;;
esac
EOF
cat >"$STUBS/docker" <<'EOF'
#!/usr/bin/env bash
echo "docker $*" >>"$STUB_LOG"
case "$1 $3" in
"inspect {{.Config.User}}") echo celestia ;;
"inspect {{.Image}}") echo sha256:feed ;;
"inspect {{.State.Running}}")
    if [ -f "$STUB_STATE/active" ]; then echo true; else echo false; fi
    ;;
stop*) rm -f "$STUB_STATE/active" ;;
start*) touch "$STUB_STATE/active" ;;
run*)
    # Run the entrypoint on the host: the test home is a host path.
    while [ "$1" != --entrypoint ]; do shift; done
    entrypoint=$2
    shift 3
    exec "$entrypoint" "$@"
    ;;
esac
EOF
cat >"$STUBS/id" <<'EOF'
#!/usr/bin/env bash
if [ -f "$STUB_STATE/root" ]; then echo 0; else exec /usr/bin/id "$@"; fi
EOF
cat >"$STUBS/curl" <<'EOF'
#!/usr/bin/env bash
out=""
for url; do
    if [ "$prev" = -o ]; then out=$url; fi
    prev=$url
done
height=$(cat "$STUB_STATE/height" 2>/dev/null || echo 20)
network=$(cat "$STUB_STATE/network" 2>/dev/null || echo test)
case "$url" in
*/genesis.json) cp "$STUB_STATE/genesis.json" "$out" ;;
*/status) echo "{\"result\":{\"node_info\":{\"network\":\"$network\"},\"sync_info\":{\"latest_block_height\":\"$height\"}}}" ;;
*/abci_info) echo '{"result":{"response":{"last_block_height":"19","app_version":"10"}}}' ;;
*/block?height=19) echo '{"result":{"block":{"header":{"app_hash":"ABCD12"}}}}' ;;
*) exit 7 ;;
esac
EOF
if ! command -v sha256sum >/dev/null 2>&1; then
    cat >"$STUBS/sha256sum" <<'EOF'
#!/usr/bin/env bash
exec shasum -a 256 "$@"
EOF
fi
chmod +x "$STUBS"/*

# new_node creates a node home with a local signer and a running service.
new_node() {
    rm -rf "$TMP/node" "$STUB_STATE" "$STUB_LOG"
    NODE_HOME=$TMP/node/home
    mkdir -p "$NODE_HOME/config" "$NODE_HOME/data" "$STUB_STATE"
    cat >"$NODE_HOME/config/config.toml" <<'EOF'
priv_validator_state_file = "data/priv_validator_state.json"
priv_validator_laddr = ""
EOF
    echo 'halt-height = 0' >"$NODE_HOME/config/app.toml"
    echo '{"height":"20","round":0,"step":3}' >"$NODE_HOME/data/priv_validator_state.json"
    touch "$STUB_STATE/active" "$STUB_LOG"
}

recovery() {
    bash "$SCRIPT" "$@" >"$TMP/out.log" 2>&1
}

fail() {
    echo "FAIL: $*" >&2
    cat "$TMP/out.log" >&2
    exit 1
}

inode() {
    stat -c %i "$1" 2>/dev/null || stat -f %i "$1"
}

called() {
    grep -qxF "$1" "$STUB_LOG"
}

sha() {
    sha256sum "$1" | awk '{ print $1 }'
}

# new_chain_node is new_node with old chain data and a new genesis for chain
# test-1 at initial height $1 (default 31), whose SHA-256 is in GENESIS_SHA.
new_chain_node() {
    new_node
    mkdir -p "$NODE_HOME/data/blockstore.db" "$NODE_HOME/data/tx_index.db" "$NODE_HOME/data/cs.wal"
    touch "$NODE_HOME/data/blockstore.db/000001.log" "$NODE_HOME/data/tx_index.db/000001.log" "$NODE_HOME/data/.lock"
    echo key >"$NODE_HOME/config/node_key.json"
    echo key >"$NODE_HOME/config/priv_validator_key.json"
    echo old >"$NODE_HOME/config/genesis.json"
    echo '{}' >"$NODE_HOME/config/addrbook.json"
    cat >>"$NODE_HOME/config/config.toml" <<'EOF2'
persistent_peers = "old@1.2.3.4:26656"
db_dir = "data"
genesis_file = "config/genesis.json"
addr_book_file = "config/addrbook.json"
wal_file = "data/cs.wal/wal"
EOF2
    printf '{"chain_id":"test-1","initial_height":"%s","genesis_time":"2026-01-01T00:00:00.123Z"}\n' "${1:-31}" \
        >"$STUB_STATE/genesis.json"
    GENESIS_SHA=$(sha "$STUB_STATE/genesis.json")
    echo 32 >"$STUB_STATE/height"
    echo test-1 >"$STUB_STATE/network"
}

new_chain() {
    recovery new-chain --home "$NODE_HOME" --genesis "$STUB_STATE/genesis.json" --genesis-sha256 "$GENESIS_SHA" \
        --chain-id test-1 "$@"
}

APPD=$STUBS/celestia-appd

# dry-run: nothing changes, the planned commands are printed.
new_node
recovery rollback --home "$NODE_HOME" --height 18 --dry-run || fail "dry-run failed"
if grep -Eq '^systemctl (stop|start)|rollback .*--height 18$' "$STUB_LOG"; then
    fail "dry-run changed state"
fi
grep -qxF "+ sudo systemctl stop celestia-appd" "$TMP/out.log" || fail "dry-run did not print the stop"
grep -qxF "+ $APPD rollback --home $NODE_HOME --height 18" "$TMP/out.log" || fail "dry-run did not print the rollback"
grep -qxF "snapshot cleanup skipped: the node is running" "$TMP/out.log" || fail "dry-run listed snapshots of a running node"

# dry-run on a stopped node runs the rollback check.
new_node
rm "$STUB_STATE/active"
recovery rollback --home "$NODE_HOME" --height 18 --dry-run || fail "dry-run on a stopped node failed"
called "$APPD rollback --home $NODE_HOME --height 18 --dry-run" || fail "dry-run skipped the rollback check"
grep -qxF "+ $APPD snapshots delete --home $NODE_HOME 19 3" "$TMP/out.log" || fail "dry-run did not print the snapshot delete"
if grep -q 'snapshots delete' "$STUB_LOG"; then
    fail "dry-run deleted a snapshot"
fi

# systemd: stop with sudo, check, roll back, delete newer snapshots, start, verify.
new_node
recovery rollback --home "$NODE_HOME" --height 18 --check-height 19 --expect-app-hash abcd12 ||
    fail "systemd rollback failed"
grep -E '^sudo |rollback|snapshots delete' "$STUB_LOG" >"$TMP/order.log"
cat >"$TMP/want.log" <<EOF
sudo systemctl stop celestia-appd
$APPD rollback --home $NODE_HOME --height 18 --dry-run
$APPD rollback --home $NODE_HOME --height 18
$APPD snapshots delete --home $NODE_HOME 19 3
sudo systemctl start celestia-appd
EOF
diff "$TMP/want.log" "$TMP/order.log" || fail "systemd call order"
grep -qxF "app hash at height 19 matches" "$TMP/out.log" || fail "app hash not verified"
[ ! -e "$NODE_HOME/data/recovery-signer.sha256" ] || fail "signer digest kept after the start"

# cosmovisor: the binary is current/bin/celestia-appd.
new_node
COSMOVISOR_BIN=$NODE_HOME/cosmovisor/current/bin/celestia-appd
mkdir -p "$(dirname "$COSMOVISOR_BIN")"
cp "$APPD" "$COSMOVISOR_BIN"
recovery rollback --setup cosmovisor --home "$NODE_HOME" --height 18 || fail "cosmovisor rollback failed"
called "$COSMOVISOR_BIN rollback --home $NODE_HOME --height 18" || fail "cosmovisor binary not used"
called "sudo systemctl stop cosmovisor" || fail "cosmovisor service not stopped"

# docker: a one-off container with the node's volumes.
new_node
recovery rollback --setup docker --container node0 --home "$NODE_HOME" --height 18 || fail "docker rollback failed"
called "docker run --rm --volumes-from node0 --user celestia --entrypoint celestia-appd sha256:feed rollback --home $NODE_HOME --height 18" ||
    fail "docker rollback command"
called "docker stop node0" || fail "docker container not stopped"
called "docker start node0" || fail "docker container not started"

# --no-service: no sudo, systemctl or docker calls.
new_node
recovery rollback --no-service --home "$NODE_HOME" --height 18 || fail "--no-service rollback failed"
if grep -Eq '^(sudo|systemctl|docker) ' "$STUB_LOG"; then
    fail "--no-service touched the service"
fi
called "$APPD rollback --home $NODE_HOME --height 18" || fail "--no-service did not roll back"

# interruption: the first rollback fails and the node stays stopped; a re-run completes.
new_node
touch "$STUB_STATE/fail-once"
if recovery rollback --home "$NODE_HOME" --height 18; then
    fail "interrupted rollback succeeded"
fi
[ ! -f "$STUB_STATE/active" ] || fail "node started after a failed rollback"
[ -s "$NODE_HOME/data/recovery-signer.sha256" ] || fail "signer digest not saved"
[ ! -e "$NODE_HOME/data/recovery-signer.sha256.tmp" ] || fail "signer digest temp file left"
recovery rollback --home "$NODE_HOME" --height 18 || fail "re-run failed"
[ -f "$STUB_STATE/active" ] || fail "node not started after the re-run"

# signer guard: a changed priv_validator_state.json fails the run before the start.
new_node
touch "$STUB_STATE/edit-signer"
if recovery rollback --home "$NODE_HOME" --height 18; then
    fail "changed signer state accepted"
fi
grep -q "signer state changed" "$TMP/out.log" || fail "signer guard message"
[ ! -f "$STUB_STATE/active" ] || fail "node started after the signer state changed"
rm "$STUB_STATE/edit-signer"
: >"$STUB_LOG"
if recovery rollback --home "$NODE_HOME" --height 18; then
    fail "re-run accepted the changed signer state"
fi
if grep -q '^sudo ' "$STUB_LOG"; then
    fail "re-run touched the service after the signer state changed"
fi

# missing local signer state: refused before the node is stopped.
new_node
rm "$NODE_HOME/data/priv_validator_state.json"
if recovery rollback --home "$NODE_HOME" --height 18; then
    fail "missing signer state accepted"
fi
grep -q "signer state not found" "$TMP/out.log" || fail "missing signer state message"
if grep -q '^sudo ' "$STUB_LOG"; then
    fail "node stopped without a signer state"
fi

# signer state removed by the rollback: the node is not started.
new_node
touch "$STUB_STATE/delete-signer"
if recovery rollback --home "$NODE_HOME" --height 18; then
    fail "removed signer state accepted"
fi
grep -q "signer state not found" "$TMP/out.log" || fail "removed signer state message"
[ ! -f "$STUB_STATE/active" ] || fail "node started without a signer state"

# remote signer: no local state needed, a warning is printed.
new_node
echo 'priv_validator_laddr = "tcp://127.0.0.1:26659"' >"$NODE_HOME/config/config.toml"
rm "$NODE_HOME/data/priv_validator_state.json"
recovery rollback --home "$NODE_HOME" --height 18 || fail "remote signer rollback failed"
grep -q "check the remote signer by hand" "$TMP/out.log" || fail "remote signer warning"

# config.toml: indented and single-quoted values are read, unquoted values are refused.
new_node
printf "  priv_validator_state_file = 'data/pv.json'  # custom\n\tpriv_validator_laddr = ''\n" \
    >"$NODE_HOME/config/config.toml"
mv "$NODE_HOME/data/priv_validator_state.json" "$NODE_HOME/data/pv.json"
recovery rollback --home "$NODE_HOME" --height 18 || fail "single-quoted config rollback failed"
grep -q "signer 20/0/3" "$TMP/out.log" || fail "custom signer state not read"
new_node
echo 'priv_validator_laddr = tcp://127.0.0.1:26659' >"$NODE_HOME/config/config.toml"
if recovery rollback --home "$NODE_HOME" --height 18; then
    fail "unquoted config value accepted"
fi
grep -q "cannot parse config.toml" "$TMP/out.log" || fail "config parse message"
new_node
echo '"priv_validator_state_file" = "data/x.json"' >"$NODE_HOME/config/config.toml"
if recovery rollback --home "$NODE_HOME" --height 18; then
    fail "quoted config key accepted"
fi
grep -q "cannot parse config.toml" "$TMP/out.log" || fail "quoted key parse message"

# halt-height and a kv tx index: warnings only.
new_node
printf 'halt-height = 25\nhalt-time = 0\n' >"$NODE_HOME/config/app.toml"
echo 'indexer = "kv"' >>"$NODE_HOME/config/config.toml"
recovery rollback --home "$NODE_HOME" --height 18 || fail "rollback with warnings failed"
grep -qxF "warning: app.toml sets halt-height = 25; the node stops again there" "$TMP/out.log" || fail "halt-height warning"
grep -q "kv tx index keeps results from before the rollback" "$TMP/out.log" || fail "kv index warning"

# restart: stop, clear halt-height and halt-time, start, verify; no rollback.
new_node
printf 'halt-height = 25\n  halt-time = 1700000000 # stop\nmin-retain-blocks = 0\n' >"$NODE_HOME/config/app.toml"
echo 'indexer = "kv"' >>"$NODE_HOME/config/config.toml"
INODE=$(inode "$NODE_HOME/config/app.toml")
recovery restart --home "$NODE_HOME" --expect-version 10.0.0 --check-height 19 --expect-app-hash abcd12 ||
    fail "restart failed"
grep -E '^sudo |rollback|snapshots' "$STUB_LOG" >"$TMP/order.log" || true
cat >"$TMP/want.log" <<EOF
sudo systemctl stop celestia-appd
sudo systemctl start celestia-appd
EOF
diff "$TMP/want.log" "$TMP/order.log" || fail "restart call order"
printf 'halt-height = 0\n  halt-time = 0\nmin-retain-blocks = 0\n' >"$TMP/want.toml"
diff "$TMP/want.toml" "$NODE_HOME/config/app.toml" || fail "restart did not clear the halt"
[ -f "$NODE_HOME/config/app.toml.bak" ] || fail "restart did not keep app.toml.bak"
[ "$(inode "$NODE_HOME/config/app.toml")" = "$INODE" ] || fail "restart replaced app.toml instead of rewriting it"
grep -qxF "app hash at height 19 matches" "$TMP/out.log" || fail "restart app hash not verified"
if grep -Eq 'the node stops again there|kv tx index' "$TMP/out.log"; then
    fail "restart printed a rollback warning"
fi

# restart --dry-run: the halt edit is printed, not made.
new_node
echo 'halt-height = 25' >"$NODE_HOME/config/app.toml"
recovery restart --home "$NODE_HOME" --dry-run || fail "restart dry-run failed"
grep -qF "+ sh -c cp \"\$1\" \"\$1.bak\" && sed -E \"\$2\" \"\$1.bak\" > \"\$1\" sh $NODE_HOME/config/app.toml" "$TMP/out.log" ||
    fail "restart dry-run did not print the halt edit"
grep -qxF 'halt-height = 25' "$NODE_HOME/config/app.toml" || fail "restart dry-run changed app.toml"
if grep -Eq '^systemctl (stop|start)' "$STUB_LOG"; then
    fail "restart dry-run changed state"
fi

# restart without a halt: app.toml is not touched.
new_node
recovery restart --home "$NODE_HOME" || fail "restart without a halt failed"
[ ! -e "$NODE_HOME/config/app.toml.bak" ] || fail "restart edited app.toml without a halt"

# restart with an old binary: refused before the node is stopped.
new_node
if recovery restart --home "$NODE_HOME" --expect-version 10.0.1; then
    fail "restart accepted the wrong version"
fi
grep -q "binary version is 10.0.0, expected 10.0.1" "$TMP/out.log" || fail "restart version message"
if grep -q '^sudo ' "$STUB_LOG"; then
    fail "restart stopped the node with the wrong version"
fi

# restart with docker: the halt is cleared in a one-off container.
new_node
echo 'halt-height = 25' >"$NODE_HOME/config/app.toml"
recovery restart --setup docker --container node0 --home "$NODE_HOME" || fail "docker restart failed"
grep -q "^docker run --rm --volumes-from node0 --user celestia --entrypoint sh sha256:feed -c cp" "$STUB_LOG" ||
    fail "docker restart halt edit"
grep -qxF 'halt-height = 0' "$NODE_HOME/config/app.toml" || fail "docker restart did not clear the halt"
called "docker start node0" || fail "docker restart did not start the container"

# new-chain: stop, move the old data aside keeping the signer state and the tx
# index, put the genesis in place, clear the halt, set peers, start, verify.
new_chain_node
echo 'halt-height = 25' >"$NODE_HOME/config/app.toml"
PEER=0123456789abcdef0123456789abcdef01234567@val-1:26656
new_chain --peers "$PEER" || fail "new-chain failed"
grep -E '^sudo ' "$STUB_LOG" >"$TMP/order.log"
cat >"$TMP/want.log" <<EOF
sudo systemctl stop celestia-appd
sudo systemctl start celestia-appd
EOF
diff "$TMP/want.log" "$TMP/order.log" || fail "new-chain call order"
BACKUP=$NODE_HOME/data/recovery-backup-${GENESIS_SHA:0:12}
(cd "$NODE_HOME/data" && ls -A) >"$TMP/data.log"
printf '%s\n' priv_validator_state.json "recovery-backup-${GENESIS_SHA:0:12}" tx_index.db | sort >"$TMP/want.log"
diff "$TMP/want.log" <(sort "$TMP/data.log") || fail "new-chain kept the wrong files"
[ -f "$NODE_HOME/data/tx_index.db/000001.log" ] || fail "new-chain lost the tx index"
[[ -f "$BACKUP/blockstore.db/000001.log" && -d "$BACKUP/cs.wal" && -f "$BACKUP/.lock" ]] || fail "old data not in the backup"
[ "$(cat "$NODE_HOME/config/priv_validator_key.json")" = key ] || fail "new-chain changed the validator key"
[ "$(cat "$BACKUP/genesis.json")" = old ] || fail "old genesis not in the backup"
[[ -f "$BACKUP/addrbook.json" && ! -e "$NODE_HOME/config/addrbook.json" ]] || fail "address book not moved"
cmp -s "$STUB_STATE/genesis.json" "$NODE_HOME/config/genesis.json" || fail "new genesis not in place"
grep -qxF 'halt-height = 0' "$NODE_HOME/config/app.toml" || fail "new-chain did not clear the halt"
grep -qxF "persistent_peers = \"$PEER\"" "$NODE_HOME/config/config.toml" || fail "new-chain did not set peers"
grep -qxF "chain-id test-1 at height 32" "$TMP/out.log" || fail "new chain not verified"

# new-chain re-run after the start: the new chain data stays, and the signer
# state above the initial height is accepted.
touch "$NODE_HOME/data/blockstore.db"
echo '{"height":"40","round":0,"step":3}' >"$NODE_HOME/data/priv_validator_state.json"
new_chain || fail "new-chain re-run failed"
[ -e "$NODE_HOME/data/blockstore.db" ] || fail "new-chain re-run moved the new chain data"
grep -q "data already replaced" "$TMP/out.log" || fail "new-chain re-run message"

# new-chain interrupted by a failed move: no genesis, no start; a re-run completes.
new_chain_node
BACKUP=$NODE_HOME/data/recovery-backup-${GENESIS_SHA:0:12}
mkdir -p "$BACKUP/blockstore.db/blocker"
if new_chain; then
    fail "new-chain with a failed move succeeded"
fi
[ ! -e "$BACKUP/.done" ] || fail "failed move marked the data as replaced"
[ "$(cat "$NODE_HOME/config/genesis.json")" = old ] || fail "failed move put the genesis in place"
[ ! -f "$STUB_STATE/active" ] || fail "node started after a failed move"
rm -r "$BACKUP/blockstore.db"
new_chain || fail "new-chain re-run after a failed move failed"
cmp -s "$STUB_STATE/genesis.json" "$NODE_HOME/config/genesis.json" || fail "re-run did not put the genesis in place"

# new-chain keeps a signer state and keys configured inside data/.
new_chain_node
mkdir -p "$NODE_HOME/data/signer keys"
mv "$NODE_HOME/data/priv_validator_state.json" "$NODE_HOME/data/signer keys/state.json"
mv "$NODE_HOME/config/node_key.json" "$NODE_HOME/data/node_key.json"
cat >"$NODE_HOME/config/config.toml" <<'EOF'
priv_validator_state_file = "./data/signer keys/state.json"
node_key_file = "data/node_key.json"
priv_validator_laddr = ""
EOF
new_chain || fail "new-chain with keys in data failed"
[[ -f "$NODE_HOME/data/signer keys/state.json" && -f "$NODE_HOME/data/node_key.json" ]] || fail "new-chain moved a key or the signer state"

# new-chain keeps a signer state given with "//" in the path or the home.
new_chain_node
echo 'priv_validator_state_file = "data//priv_validator_state.json"' >"$NODE_HOME/config/config.toml"
recovery new-chain --home "$TMP/node//home/" --genesis "$STUB_STATE/genesis.json" --genesis-sha256 "$GENESIS_SHA" \
    --chain-id test-1 || fail "new-chain with // paths failed"
[ -f "$NODE_HOME/data/priv_validator_state.json" ] || fail "new-chain with // paths moved the signer state"

# new-chain accepts default paths written another way.
new_chain_node
sed -i.bak "s|^db_dir = .*|db_dir = \"$NODE_HOME/data/\"|" "$NODE_HOME/config/config.toml"
echo 'blockstore_dir = ""' >>"$NODE_HOME/config/config.toml"
new_chain || fail "new-chain refused default paths written another way"

# new-chain: the node signs the initial height while it stops; nothing is replaced.
new_chain_node
echo "$NODE_HOME/data/priv_validator_state.json" >"$STUB_STATE/sign-on-stop"
if new_chain; then
    fail "new-chain accepted a signer state reaching the initial height at the stop"
fi
grep -qF "signer state is at height 31" "$TMP/out.log" || fail "new-chain stop signer message"
[ "$(cat "$NODE_HOME/config/genesis.json")" = old ] || fail "new-chain replaced the genesis after the stop signer check"
[ -f "$NODE_HOME/data/blockstore.db/000001.log" ] || fail "new-chain moved data after the stop signer check"
! called "systemctl start celestia-appd" || fail "new-chain started the node after the stop signer check"

# new-chain refuses a relative home and key paths with "..".
new_chain_node
(cd "$TMP/node" && bash "$SCRIPT" new-chain --home home --genesis "$STUB_STATE/genesis.json" \
    --genesis-sha256 "$GENESIS_SHA" --chain-id test-1 >"$TMP/out.log" 2>&1) && fail "relative home accepted"
grep -q "absolute --home" "$TMP/out.log" || fail "relative home message"
recovery new-chain --home // --genesis "$STUB_STATE/genesis.json" --genesis-sha256 "$GENESIS_SHA" \
    --chain-id test-1 && fail "root home accepted"
grep -q "other than /" "$TMP/out.log" || fail "root home message"
echo 'node_key_file = "data/../config/node_key.json"' >>"$NODE_HOME/config/config.toml"
if new_chain; then
    fail "key path with .. accepted"
fi
grep -q "path with .. is not supported" "$TMP/out.log" || fail "key path with .. message"

# new-chain from a URL.
new_chain_node
recovery new-chain --home "$NODE_HOME" --genesis https://example.com/genesis.json --genesis-sha256 "$GENESIS_SHA" \
    --chain-id test-1 || fail "new-chain from a URL failed"
cmp -s "$STUB_STATE/genesis.json" "$NODE_HOME/config/genesis.json" || fail "downloaded genesis not in place"

# new-chain refusals before the node is stopped: wrong hash, wrong chain-id,
# signer state at the initial height, bad peers, a custom or empty db_dir, a
# custom blockstore_dir, a wal_file outside data/ or in a kept entry.
for c in sha chain-id signer peers db_dir empty-db_dir blockstore_dir wal wal-kept; do
    new_chain_node
    args=()
    case "$c" in
    sha) GENESIS_SHA=$(printf '0%.0s' {1..64}); want="genesis SHA-256 is" ;;
    chain-id) sed -i.bak 's/test-1/test-2/' "$STUB_STATE/genesis.json"; GENESIS_SHA=$(sha "$STUB_STATE/genesis.json"); want="genesis chain-id is test-2" ;;
    signer) new_chain_node 20; want="signer state is at height 20" ;;
    peers) args=(--peers 'x@y:1"'); want="--peers must be" ;;
    db_dir) sed -i.bak 's|^db_dir = .*|db_dir = "/srv/db"|' "$NODE_HOME/config/config.toml"; want="only the default db_dir" ;;
    empty-db_dir) sed -i.bak 's|^db_dir = .*|db_dir = ""|' "$NODE_HOME/config/config.toml"; want="only the default db_dir" ;;
    blockstore_dir) echo 'blockstore_dir = "/srv/blocks"' >>"$NODE_HOME/config/config.toml"; want="only the default blockstore_dir" ;;
    wal) sed -i.bak 's|^wal_file = .*|wal_file = "/srv/wal"|' "$NODE_HOME/config/config.toml"; want="needs wal_file in data/" ;;
    wal-kept) sed -i.bak 's|^wal_file = .*|wal_file = "data/tx_index.db/wal"|' "$NODE_HOME/config/config.toml"; want="needs wal_file in data/" ;;
    esac
    if new_chain ${args[@]+"${args[@]}"}; then
        fail "new-chain accepted: $c"
    fi
    grep -qF -- "$want" "$TMP/out.log" || fail "new-chain $c message"
    if grep -q '^sudo ' "$STUB_LOG"; then
        fail "new-chain stopped the node: $c"
    fi
    [ "$(cat "$NODE_HOME/config/genesis.json")" = old ] || fail "new-chain replaced the genesis: $c"
done

# new-chain --dry-run: nothing moves.
new_chain_node
new_chain --dry-run || fail "new-chain dry-run failed"
[[ -d "$NODE_HOME/data/blockstore.db" && "$(cat "$NODE_HOME/config/genesis.json")" = old ]] ||
    fail "new-chain dry-run changed data"
if grep -Eq '^systemctl (stop|start)' "$STUB_LOG"; then
    fail "new-chain dry-run changed state"
fi

# new-chain with docker: the genesis is written in a one-off container.
new_chain_node
new_chain --setup docker --container node0 || fail "docker new-chain failed"
grep -q "^docker run --rm --volumes-from node0 --user celestia -i --entrypoint sh sha256:feed -c set -e; cat" "$STUB_LOG" ||
    fail "docker new-chain genesis write"
cmp -s "$STUB_STATE/genesis.json" "$NODE_HOME/config/genesis.json" || fail "docker new-chain genesis not in place"

# new-chain --no-service: the node is not started or checked.
new_chain_node
new_chain --no-service || fail "--no-service new-chain failed"
grep -qF "then run: recovery.sh verify --chain-id test-1 --check-height 32" "$TMP/out.log" || fail "--no-service new-chain message"

# verify --chain-id: a match passes, another chain-id exits 1.
recovery verify --check-height 32 --chain-id test-1 || fail "matching chain-id failed"
status=0
recovery verify --check-height 32 --chain-id test-2 || status=$?
[ "$status" = 1 ] || fail "chain-id mismatch exited $status"

# --height 0 means "current height - 1" to the binary, so it is refused.
new_node
if recovery rollback --home "$NODE_HOME" --height 0; then
    fail "--height 0 accepted"
fi
grep -q "positive number" "$TMP/out.log" || fail "--height 0 message"

# root: a home owned by another user is refused before anything runs.
new_node
touch "$STUB_STATE/root"
if recovery rollback --home "$NODE_HOME" --height 18; then
    fail "root run accepted"
fi
grep -q "not as root" "$TMP/out.log" || fail "root guard message"
[ ! -s "$STUB_LOG" ] || fail "root run called a command"

# verify: a matching hash passes, a mismatch exits 1.
new_node
recovery verify --check-height 19 --expect-app-hash abcd12 || fail "matching app hash failed"
status=0
recovery verify --check-height 19 --expect-app-hash ffff || status=$?
[ "$status" = 1 ] || fail "app hash mismatch exited $status"

echo "recovery script checks passed"
