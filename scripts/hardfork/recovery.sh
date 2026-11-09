#!/usr/bin/env bash
# Validator recovery: roll back or restart a node, move it to a new chain, or
# verify it.
# Run "recovery.sh --help" for usage.

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: recovery.sh <mode> [flags]

Modes:
  rollback   stop the node, roll back to --height, delete the snapshots
             above it, start the node
  restart    stop the node, set halt-height and halt-time to 0, start the
             node; install the fixed release first
  new-chain  check the new genesis, stop the node, move the old chain data
             aside, put the genesis in place, start the node and wait for
             new blocks
  verify     wait for --check-height and compare its app hash or chain-id

Common flags:
  --home DIR             node home as the binary sees it (default ~/.celestia-app)
  --setup KIND           systemd, cosmovisor, docker or none (default systemd)
  --no-service           same as --setup none: do not stop or start the node
  --service NAME         systemd unit (default celestia-appd; cosmovisor: cosmovisor)
  --container NAME       docker container of the node
  --binary PATH          celestia-appd (default: on PATH; cosmovisor:
                         $DAEMON_HOME/cosmovisor/current/bin/celestia-appd,
                         DAEMON_HOME defaults to --home)
  --rpc URL              node RPC (default http://localhost:26657)
  --expect-version V     fail if the binary version is not V
  --dry-run              print state-changing commands instead of running them

rollback:
  --height X             height to roll back to (required)
  --force                allow a deep rollback
  --check-height H       after the start, check the app hash at height H
  --expect-app-hash HEX  expected app hash at --check-height

restart:
  --check-height H       after the start, check the app hash at height H
  --expect-app-hash HEX  expected app hash at --check-height

new-chain:
  --genesis FILE|URL     new genesis.json (required)
  --genesis-sha256 HEX   published SHA-256 of the genesis (required)
  --chain-id ID          chain-id of the new chain (required)
  --peers LIST           persistent_peers to set, id@host:port,...
  --genesis is read where the script runs, on the host with docker.

  The node keeps its keys, the signer state and the tx index. The rest of
  data/ moves to data/recovery-backup-<sha>; delete it once the new chain runs.
  The signer state must be below the initial height of the genesis.

verify:
  --check-height H       height to check (required)
  --expect-app-hash HEX  expected app hash
  --chain-id ID          expected chain-id
  One of --expect-app-hash and --chain-id is required.

With docker, paths are inside the container, and commands run in a one-off
container with the node's volumes. After an interruption, run again with the same flags.
EOF
}

MODE=""
NODE_HOME=$HOME/.celestia-app
SETUP=systemd
SERVICE=""
CONTAINER=""
BIN=""
RPC=http://localhost:26657
EXPECT_VERSION=""
DRY_RUN=0
HEIGHT=""
FORCE=0
CHECK_HEIGHT=""
EXPECT_APP_HASH=""
GENESIS=""
GENESIS_SHA=""
CHAIN_ID=""
PEERS=""

die() {
    echo "error: $*" >&2
    exit 1
}

parse_args() {
    if [ $# -eq 0 ]; then
        usage
        exit 1
    fi
    case "$1" in
    -h | --help)
        usage
        exit 0
        ;;
    esac
    MODE=$1
    shift
    while [ $# -gt 0 ]; do
        case "$1" in
        --dry-run) DRY_RUN=1; shift; continue ;;
        --force) FORCE=1; shift; continue ;;
        --no-service) SETUP=none; shift; continue ;;
        esac
        [ $# -ge 2 ] || die "$1 needs a value"
        case "$1" in
        --home) NODE_HOME=${2%/} ;;
        --setup) SETUP=$2 ;;
        --service) SERVICE=$2 ;;
        --container) CONTAINER=$2 ;;
        --binary) BIN=$2 ;;
        --rpc) RPC=$2 ;;
        --expect-version) EXPECT_VERSION=$2 ;;
        --height) HEIGHT=$2 ;;
        --check-height) CHECK_HEIGHT=$2 ;;
        --expect-app-hash) EXPECT_APP_HASH=$2 ;;
        --genesis) GENESIS=$2 ;;
        --genesis-sha256) GENESIS_SHA=$2 ;;
        --chain-id) CHAIN_ID=$2 ;;
        --peers) PEERS=$2 ;;
        *) die "unknown flag: $1" ;;
        esac
        shift 2
    done
}

check_args() {
    case "$MODE" in
    rollback) [[ "$HEIGHT" =~ ^[1-9][0-9]*$ ]] || die "rollback needs --height, a positive number" ;;
    restart) ;;
    new-chain)
        if [ -z "$GENESIS" ] || [ -z "$CHAIN_ID" ]; then
            die "new-chain needs --genesis, --genesis-sha256 and --chain-id"
        fi
        if [[ "$NODE_HOME" != /* || "$NODE_HOME/" == *"/./"* || "$NODE_HOME/" == *"/../"* ]]; then
            die "new-chain needs an absolute --home without . or .."
        fi
        while [[ "$NODE_HOME" == *"//"* ]]; do
            NODE_HOME=${NODE_HOME//\/\//\/}
        done
        NODE_HOME=${NODE_HOME%/}
        [ -n "$NODE_HOME" ] || die "new-chain needs a --home other than /"
        [[ "$GENESIS_SHA" =~ ^[0-9a-fA-F]{64}$ ]] || die "--genesis-sha256 must be 64 hex characters"
        GENESIS_SHA=$(lower "$GENESIS_SHA")
        if [ -n "$PEERS" ] && ! [[ "$PEERS" =~ ^[0-9a-f]{40}@[A-Za-z0-9.:_-]+:[0-9]+(,[0-9a-f]{40}@[A-Za-z0-9.:_-]+:[0-9]+)*$ ]]; then
            die "--peers must be id@host:port,..."
        fi
        ;;
    verify)
        if [ -z "$CHECK_HEIGHT" ] || [ -z "$EXPECT_APP_HASH$CHAIN_ID" ]; then
            die "verify needs --check-height and --expect-app-hash or --chain-id"
        fi
        ;;
    *) die "unknown mode: $MODE" ;;
    esac
    if [ "$MODE" != verify ] && [ -n "$CHECK_HEIGHT$EXPECT_APP_HASH" ] && { [ -z "$CHECK_HEIGHT" ] || [ -z "$EXPECT_APP_HASH" ]; }; then
        die "--check-height and --expect-app-hash go together"
    fi
    if [ -n "$CHECK_HEIGHT" ] && ! [[ "$CHECK_HEIGHT" =~ ^[1-9][0-9]*$ ]]; then
        die "--check-height must be a positive number"
    fi
    case "$SETUP" in
    systemd) SERVICE=${SERVICE:-celestia-appd} ;;
    cosmovisor) SERVICE=${SERVICE:-cosmovisor} ;;
    docker) [ -n "$CONTAINER" ] || die "--setup docker needs --container" ;;
    none) ;;
    *) die "unknown setup: $SETUP" ;;
    esac
    if [ "$SETUP" = docker ]; then
        BIN=celestia-appd
    elif [ -z "$BIN" ] && [ "$MODE" != verify ]; then
        if [ "$SETUP" = cosmovisor ]; then
            BIN=${DAEMON_HOME:-$NODE_HOME}/cosmovisor/current/bin/celestia-appd
        else
            BIN=$(command -v celestia-appd) || die "celestia-appd not found on PATH; use --binary"
        fi
    fi
    SYSTEMCTL=(systemctl)
    if [ "$(id -u)" != 0 ]; then
        SYSTEMCTL=(sudo systemctl)
    fi
    if [ "$MODE" = rollback ]; then
        ROLLBACK_ARGS=(rollback --home "$NODE_HOME" --height "$HEIGHT")
        if [ "$FORCE" = 1 ]; then
            ROLLBACK_ARGS+=(--force)
        fi
    fi
}

setup_docker() {
    local user
    IMAGE=$(docker inspect -f '{{.Image}}' "$CONTAINER")
    user=$(docker inspect -f '{{.Config.User}}' "$CONTAINER")
    DOCKER_RUN=(docker run --rm --volumes-from "$CONTAINER")
    if [ -n "$user" ]; then
        DOCKER_RUN+=(--user "$user")
    fi
}

# check_user refuses to run as root on a home owned by another user: root-owned
# DB files would stop the node from starting.
check_user() {
    local owner
    owner=$(stat -c %u "$NODE_HOME" 2>/dev/null || stat -f %u "$NODE_HOME")
    if [ "$(id -u)" = 0 ] && [ "$owner" != 0 ]; then
        die "run as the owner of $NODE_HOME (uid $owner), not as root"
    fi
}

# in_node runs a command as the node sees it: on the host, or in a one-off
# container with the node's volumes.
in_node() {
    if [ "$SETUP" = docker ]; then
        "${DOCKER_RUN[@]}" --entrypoint "$1" "$IMAGE" "${@:2}"
    else
        "$@"
    fi
}

run_bin() {
    in_node "$BIN" "$@"
}

# in_node_stdin is in_node with stdin passed to the command.
in_node_stdin() {
    if [ "$SETUP" = docker ]; then
        "${DOCKER_RUN[@]}" -i --entrypoint "$1" "$IMAGE" "${@:2}"
    else
        "$@"
    fi
}

# change prints a state-changing command and runs it unless --dry-run is set.
change() {
    local cmd=("$@")
    case "$1" in
    in_node | in_node_stdin) cmd=("${@:2}") ;;
    run_bin) cmd=("$BIN" "${@:2}") ;;
    esac
    echo "+ ${cmd[*]}"
    if [ "$DRY_RUN" = 0 ]; then
        "$@"
    fi
}

# toml_value prints a quoted string value from CONFIG, or nothing if the key is absent.
# Any other line that mentions the key aborts.
toml_value() {
    local line value="" re="^[[:space:]]*$1[[:space:]]*=[[:space:]]*(\"([^\"]*)\"|'([^']*)')[[:space:]]*(#.*)?$"
    while IFS= read -r line; do
        [[ "$line" =~ $re ]] || die "cannot parse config.toml: $line"
        value=${BASH_REMATCH[2]}${BASH_REMATCH[3]}
    done < <(grep -v '^[[:space:]]*#' <<<"$CONFIG" | grep -F "$1")
    echo "$value"
}

load_config() {
    CONFIG=$(in_node cat "$NODE_HOME/config/config.toml")
    PV_STATE=$(toml_value priv_validator_state_file)
    PV_STATE=${PV_STATE:-data/priv_validator_state.json}
    case "$PV_STATE" in
    /*) ;;
    *) PV_STATE=$NODE_HOME/$PV_STATE ;;
    esac
    REMOTE_SIGNER=$(toml_value priv_validator_laddr)
    INDEXER=$(toml_value indexer)
    SIGNER_SUM=$NODE_HOME/data/recovery-signer.sha256
    HALT=$(in_node cat "$NODE_HOME/config/app.toml" |
        grep -E '^[[:space:]]*halt-(height|time)[[:space:]]*=' |
        grep -Ev '=[[:space:]]*0[[:space:]]*(#.*)?$' || true)
}

# require_signer_state aborts if a local signer has no readable state file.
require_signer_state() {
    if [ -z "$REMOTE_SIGNER" ] && ! in_node test -r "$PV_STATE"; then
        die "signer state not found: $PV_STATE"
    fi
}

signer_hash() {
    in_node sha256sum "$PV_STATE" | awk '{ print $1 }'
}

# save_signer_digest saves the signer state digest before the first rollback,
# so a re-run compares against the state before any rollback.
save_signer_digest() {
    if [ -n "$REMOTE_SIGNER" ] || [ "$DRY_RUN" = 1 ] || in_node test -f "$SIGNER_SUM"; then
        return 0
    fi
    # shellcheck disable=SC2016
    in_node sh -c 'sha256sum "$1" > "$2.tmp" && mv "$2.tmp" "$2"' sh "$PV_STATE" "$SIGNER_SUM"
}

# check_signer_digest aborts if the signer state differs from the saved digest.
check_signer_digest() {
    local saved
    if [ -n "$REMOTE_SIGNER" ] || ! in_node test -f "$SIGNER_SUM"; then
        return 0
    fi
    saved=$(in_node cat "$SIGNER_SUM" | awk '{ print $1 }')
    if [ "$(signer_hash)" != "$saved" ]; then
        echo "error: signer state changed since $SIGNER_SUM was saved; do not start the node" >&2
        exit 1
    fi
}

svc_running() {
    case "$SETUP" in
    systemd | cosmovisor) systemctl is-active --quiet "$SERVICE" ;;
    docker) [ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER")" = true ] ;;
    *) return 1 ;;
    esac
}

svc_stop() {
    case "$SETUP" in
    systemd | cosmovisor) change "${SYSTEMCTL[@]}" stop "$SERVICE" ;;
    docker) change docker stop "$CONTAINER" ;;
    *) return 0 ;;
    esac
    if [ "$DRY_RUN" = 1 ]; then
        return 0
    fi
    for _ in $(seq 120); do
        if ! svc_running; then
            return 0
        fi
        sleep 1
    done
    die "the node did not stop"
}

svc_start() {
    case "$SETUP" in
    systemd | cosmovisor) change "${SYSTEMCTL[@]}" start "$SERVICE" ;;
    docker) change docker start "$CONTAINER" ;;
    esac
}

precheck() {
    local version status info blocks="?" app="?" app_version="?" signer="remote, not checked" free
    version=$(run_bin version)
    if [ -n "$EXPECT_VERSION" ] && [ "${version#v}" != "${EXPECT_VERSION#v}" ]; then
        die "binary version is $version, expected $EXPECT_VERSION"
    fi
    if status=$(curl -sf -m 5 "$RPC/status") && info=$(curl -sf -m 5 "$RPC/abci_info"); then
        blocks=$(jq -r .result.sync_info.latest_block_height <<<"$status")
        app=$(jq -r .result.response.last_block_height <<<"$info")
        app_version=$(jq -r .result.response.app_version <<<"$info")
    fi
    if [ -z "$REMOTE_SIGNER" ]; then
        require_signer_state
        signer=$(in_node jq -r '"\(.height)/\(.round)/\(.step)"' "$PV_STATE")
        check_signer_digest
    fi
    echo "blockstore $blocks, app $app (v$app_version), signer $signer, binary $version"
    if [ -n "$REMOTE_SIGNER" ]; then
        echo "warning: signer state not checked; check the remote signer by hand"
    fi
    if [ -n "$HALT" ] && [ "$MODE" = rollback ]; then
        echo "warning: app.toml sets $HALT; the node stops again there"
    fi
    if [ "$INDEXER" = kv ] && [ "$MODE" = rollback ]; then
        echo "warning: the kv tx index keeps results from before the rollback; tx and block search above height $HEIGHT can be wrong"
    fi
    free=$(in_node df -Pk "$NODE_HOME/data" | awk 'NR == 2 { print $4 }')
    [ "$free" -ge $((1024 * 1024)) ] || die "less than 1 GiB free in $NODE_HOME/data"
}

rpc_height() {
    local height
    height=$(curl -sf -m 5 "$RPC/status" | jq -r .result.sync_info.latest_block_height) || height=0
    [[ "$height" =~ ^[0-9]+$ ]] || height=0
    echo "$height"
}

upper() {
    tr '[:lower:]' '[:upper:]' <<<"$1"
}

lower() {
    tr '[:upper:]' '[:lower:]' <<<"$1"
}

# verify_app_hash waits for CHECK_HEIGHT, then compares its app hash with
# EXPECT_APP_HASH and the node's chain-id with CHAIN_ID, if they are set.
# WAIT seconds is how long it waits (default 600).
verify_app_hash() {
    local deadline=$((SECONDS + ${WAIT:-600})) got
    echo "waiting for height $CHECK_HEIGHT"
    until [ "$(rpc_height)" -ge "$CHECK_HEIGHT" ]; do
        [ "$SECONDS" -lt "$deadline" ] || die "the node did not reach height $CHECK_HEIGHT"
        sleep 2
    done
    if [ -n "$CHAIN_ID" ]; then
        got=$(curl -sf -m 5 "$RPC/status" | jq -r .result.node_info.network)
        [ "$got" = "$CHAIN_ID" ] || die "the node is on chain-id $got, expected $CHAIN_ID"
        echo "chain-id $CHAIN_ID at height $CHECK_HEIGHT"
    fi
    if [ -n "$EXPECT_APP_HASH" ]; then
        got=$(curl -sf -m 5 "$RPC/block?height=$CHECK_HEIGHT" | jq -r .result.block.header.app_hash)
        if [ "$(upper "$got")" != "$(upper "$EXPECT_APP_HASH")" ]; then
            die "app hash at height $CHECK_HEIGHT is $got, expected $EXPECT_APP_HASH"
        fi
        echo "app hash at height $CHECK_HEIGHT matches"
    fi
}

# delete_snapshots deletes the local state sync snapshots above HEIGHT. They hold
# the state from before the rollback, and peers that restore one fail state sync.
delete_snapshots() {
    local list height format
    if [ "$DRY_RUN" = 1 ] && svc_running; then
        echo "snapshot cleanup skipped: the node is running"
        return 0
    fi
    list=$(run_bin snapshots list --home "$NODE_HOME" 2>&1) || die "snapshots list failed: $list"
    while read -r _ height _ format _; do
        if [ "$height" -gt "$HEIGHT" ]; then
            change run_bin snapshots delete --home "$NODE_HOME" "$height" "$format"
        fi
    done < <(grep '^height: ' <<<"$list" || true)
}

mode_rollback() {
    precheck
    svc_stop
    if [ "$DRY_RUN" = 1 ] && svc_running; then
        echo "rollback check skipped: the node is running; stop it to run the check"
    else
        run_bin "${ROLLBACK_ARGS[@]}" --dry-run || die "rollback check failed"
    fi
    save_signer_digest
    change run_bin "${ROLLBACK_ARGS[@]}"
    delete_snapshots
    require_signer_state
    check_signer_digest
    svc_start
    if [ -z "$REMOTE_SIGNER" ]; then
        change in_node rm -f "$SIGNER_SUM"
    fi
    verify_after_start
}

# clear_halt sets halt-height and halt-time to 0 in app.toml, so the node does
# not stop again at the old halt. The previous file is kept as app.toml.bak.
# The file is rewritten in place, so a bind-mounted app.toml keeps working.
clear_halt() {
    if [ -z "$HALT" ]; then
        return 0
    fi
    # shellcheck disable=SC2016
    change in_node sh -c 'cp "$1" "$1.bak" && sed -E "$2" "$1.bak" > "$1"' sh "$NODE_HOME/config/app.toml" \
        's/^([[:space:]]*halt-(height|time)[[:space:]]*=).*/\1 0/'
}

mode_restart() {
    precheck
    svc_stop
    clear_halt
    svc_start
    verify_after_start
}

# verify_after_start checks the app hash at CHECK_HEIGHT once the node runs.
verify_after_start() {
    if [ -z "$EXPECT_APP_HASH" ] || [ "$DRY_RUN" = 1 ]; then
        return 0
    fi
    if [ "$SETUP" = none ]; then
        echo "app hash not checked: start the node, then run verify"
    else
        verify_app_hash
    fi
}

# fetch_genesis downloads or copies the new genesis to GENESIS_FILE, checks its
# SHA-256 and chain-id, and reads its initial height and genesis time.
fetch_genesis() {
    local sum
    GENESIS_FILE=$(mktemp)
    trap 'rm -f "$GENESIS_FILE"' EXIT
    case "$GENESIS" in
    http://* | https://*) curl -fsSL -m 600 -o "$GENESIS_FILE" "$GENESIS" || die "failed to download $GENESIS" ;;
    *) cp "$GENESIS" "$GENESIS_FILE" || die "cannot read $GENESIS" ;;
    esac
    sum=$(sha256sum "$GENESIS_FILE" | awk '{ print $1 }')
    [ "$sum" = "$GENESIS_SHA" ] || die "genesis SHA-256 is $sum, expected $GENESIS_SHA"
    read -r GENESIS_CHAIN_ID INITIAL_HEIGHT GENESIS_TIME < <(jq -r \
        '[.chain_id, (.initial_height | tonumber), (.genesis_time | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601)] | @tsv' \
        "$GENESIS_FILE") || die "cannot parse the genesis"
    [ "$GENESIS_CHAIN_ID" = "$CHAIN_ID" ] || die "genesis chain-id is $GENESIS_CHAIN_ID, expected $CHAIN_ID"
    [[ "$INITIAL_HEIGHT" =~ ^[1-9][0-9]*$ ]] || die "genesis initial height is $INITIAL_HEIGHT"
    echo "genesis $GENESIS_SHA: chain-id $CHAIN_ID, initial height $INITIAL_HEIGHT"
    BACKUP=$NODE_HOME/data/recovery-backup-${GENESIS_SHA:0:12}
}

# check_signer_height refuses a local signer state at or above the initial
# height: the node would not sign there. Resetting it instead could let the
# validator sign an old height again.
check_signer_height() {
    local signed
    if [ -n "$REMOTE_SIGNER" ]; then
        echo "warning: check by hand that the remote signer state is below height $INITIAL_HEIGHT"
        return 0
    fi
    signed=$(in_node jq -r .height "$PV_STATE")
    [[ "$signed" =~ ^[0-9]+$ ]] || die "cannot read the height in $PV_STATE"
    if [ "$signed" -ge "$INITIAL_HEIGHT" ]; then
        die "signer state is at height $signed, not below the initial height $INITIAL_HEIGHT"
    fi
}

# home_path prints a config.toml path as an absolute path without "//" or "./" parts.
# A path with ".." is refused: its data/ entry cannot be told safely.
home_path() {
    local path=$1
    case "$path" in
    /*) ;;
    *) path=$NODE_HOME/$path ;;
    esac
    [[ "$path/" != *"/../"* ]] || die "config path with .. is not supported: $1"
    while [[ "$path" == *"//"* ]]; do
        path=${path//\/\//\/}
    done
    while [[ "$path" == *"/./"* ]]; do
        path=${path//\/.\//\/}
    done
    echo "$path"
}

# data_entry prints the entry of data/ that holds path, or nothing.
data_entry() {
    local rest
    case "$1" in
    "$NODE_HOME/data/"*) rest=${1#"$NODE_HOME/data/"}; echo "${rest%%/*}" ;;
    esac
}

# load_keys reads the key paths from config.toml and lists the data/ entries
# that replace_data keeps: the tx index and any entry holding a key or the
# signer state.
load_keys() {
    PV_KEY=$(toml_value priv_validator_key_file)
    PV_KEY=$(home_path "${PV_KEY:-config/priv_validator_key.json}")
    NODE_KEY=$(toml_value node_key_file)
    NODE_KEY=$(home_path "${NODE_KEY:-config/node_key.json}")
    PV_STATE=$(home_path "$PV_STATE")
    KEEP=(tx_index.db "$(data_entry "$PV_STATE")" "$(data_entry "$PV_KEY")" "$(data_entry "$NODE_KEY")")
}

# toml_has succeeds if config.toml sets the key, even to "".
toml_has() {
    grep -Eq "^[[:space:]]*$1[[:space:]]*=" <<<"$CONFIG"
}

# check_layout refuses config.toml paths that replace_data does not handle.
# It runs after load_keys: the WAL must not be in a kept entry.
check_layout() {
    local key want value entry k
    for key in db_dir=data blockstore_dir=data genesis_file=config/genesis.json addr_book_file=config/addrbook.json; do
        want=${key#*=}
        key=${key%%=*}
        toml_has "$key" || continue
        value=$(toml_value "$key")
        # An empty blockstore_dir means db_dir.
        if [ -z "$value" ] && [ "$key" = blockstore_dir ]; then
            continue
        fi
        value=$(home_path "$value")
        [ "${value%/}" = "$NODE_HOME/$want" ] || die "new-chain supports only the default $key, not \"$(toml_value "$key")\""
    done
    value=data/cs.wal/wal
    if toml_has wal_file; then
        value=$(toml_value wal_file)
    fi
    entry=$(data_entry "$(home_path "$value")")
    for k in "${KEEP[@]}"; do
        if [ "$entry" = "$k" ]; then
            entry=""
        fi
    done
    [ -n "$entry" ] || die "new-chain needs wal_file in data/, apart from the keys and the signer state, not \"$value\""
}

# require_keys aborts if the node key, or the key and state of a local signer,
# is missing.
require_keys() {
    in_node test -r "$NODE_KEY" || die "node key not found: $NODE_KEY"
    if [ -z "$REMOTE_SIGNER" ]; then
        in_node test -r "$PV_KEY" || die "validator key not found: $PV_KEY"
    fi
    require_signer_state
}

# replace_data moves the old chain data to BACKUP, keeping the entries in KEEP,
# and puts the new genesis in place. BACKUP/.done marks it done, so a re-run
# after the node started on the new chain keeps the new data.
replace_data() {
    # shellcheck disable=SC2016
    change in_node sh -c 'set -e
    data=$1 backup=$2 config=$3
    shift 3
    mkdir -p "$backup"
    for f in "$data"/* "$data"/.[!.]* "$data"/..?*; do
        if [ ! -e "$f" ] && [ ! -L "$f" ]; then continue; fi
        skip=0
        for k in "$@"; do
            if [ -n "$k" ] && [ "$f" = "$data/$k" ]; then skip=1; fi
        done
        if [ "$f" = "$backup" ] || [ "$skip" = 1 ]; then continue; fi
        mv "$f" "$backup/"
    done
    for f in genesis.json addrbook.json; do
        if [ -f "$config/$f" ] && [ ! -e "$backup/$f" ]; then cp "$config/$f" "$backup/$f"; fi
    done
    rm -f "$config/addrbook.json"' sh "$NODE_HOME/data" "$BACKUP" "$NODE_HOME/config" "${KEEP[@]}"
    # shellcheck disable=SC2016
    change in_node_stdin sh -c 'set -e; cat > "$1.tmp"; mv "$1.tmp" "$1"; touch "$2/.done"' sh \
        "$NODE_HOME/config/genesis.json" "$BACKUP" <"$GENESIS_FILE"
    echo "old chain data moved to $BACKUP; delete it once the new chain runs"
}

# set_peers sets persistent_peers in config.toml to PEERS. The edit is made in a
# temporary file first, then copied over config.toml, which keeps its inode.
set_peers() {
    if [ -z "$PEERS" ]; then
        return 0
    fi
    # shellcheck disable=SC2016
    change in_node sh -c 'set -e; sed -E "$2" "$1" > "$1.tmp"; cp "$1" "$1.bak"; cat "$1.tmp" > "$1"; rm "$1.tmp"' sh \
        "$NODE_HOME/config/config.toml" "s|^([[:space:]]*persistent_peers[[:space:]]*=).*|\\1 \"$PEERS\"|"
}

mode_new_chain() {
    local done=0
    fetch_genesis
    load_keys
    check_layout
    precheck
    require_keys
    if in_node test -f "$BACKUP/.done"; then
        done=1
        echo "data already replaced, old data in $BACKUP"
    else
        check_signer_height
    fi
    svc_stop
    if [ "$done" = 0 ]; then
        # Check again: the node may have signed until it stopped.
        check_signer_height
        replace_data
    fi
    require_keys
    clear_halt
    set_peers
    svc_start
    if [ "$DRY_RUN" = 1 ]; then
        return 0
    fi
    CHECK_HEIGHT=$((INITIAL_HEIGHT + 1))
    if [ "$SETUP" = none ]; then
        echo "start the node, then run: recovery.sh verify --chain-id $CHAIN_ID --check-height $CHECK_HEIGHT"
        return 0
    fi
    # The chain starts at the genesis time, once 2/3 of the voting power is online.
    WAIT=$((GENESIS_TIME - $(date +%s) + 1800))
    if [ "$WAIT" -lt 600 ]; then
        WAIT=600
    fi
    verify_app_hash
}

mode_verify() {
    verify_app_hash
}

main() {
    parse_args "$@"
    check_args
    echo "recovery.sh sha256 $(sha256sum "${BASH_SOURCE[0]}" | awk '{ print $1 }')"
    if [ "$SETUP" = docker ]; then
        setup_docker
    elif [ "$MODE" != verify ]; then
        check_user
    fi
    if [ "$MODE" != verify ]; then
        load_config
    fi
    "mode_${MODE//-/_}"
}

main "$@"
