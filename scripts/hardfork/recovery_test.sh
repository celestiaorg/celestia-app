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
stop) rm -f "$STUB_STATE/active" ;;
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
for url; do :; done
case "$url" in
*/status) echo '{"result":{"sync_info":{"latest_block_height":"20"}}}' ;;
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

called() {
    grep -qxF "$1" "$STUB_LOG"
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
