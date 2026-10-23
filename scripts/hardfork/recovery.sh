#!/usr/bin/env bash
# Validator recovery: roll back a node or verify its app hash.
# Run "recovery.sh --help" for usage.

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: recovery.sh <mode> [flags]

Modes:
  rollback   stop the node, roll back to --height, start the node
  verify     wait for --check-height and compare its app hash

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

verify:
  --check-height H       height to check (required)
  --expect-app-hash HEX  expected app hash (required)

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
        *) die "unknown flag: $1" ;;
        esac
        shift 2
    done
}

check_args() {
    case "$MODE" in
    rollback) [[ "$HEIGHT" =~ ^[1-9][0-9]*$ ]] || die "rollback needs --height, a positive number" ;;
    verify) [ -n "$CHECK_HEIGHT" ] || die "verify needs --check-height and --expect-app-hash" ;;
    *) die "unknown mode: $MODE" ;;
    esac
    if [ -n "$CHECK_HEIGHT$EXPECT_APP_HASH" ] && { [ -z "$CHECK_HEIGHT" ] || [ -z "$EXPECT_APP_HASH" ]; }; then
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

# change prints a state-changing command and runs it unless --dry-run is set.
change() {
    local cmd=("$@")
    case "$1" in
    in_node) cmd=("${@:2}") ;;
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
    SIGNER_SUM=$NODE_HOME/data/recovery-signer.sha256
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

# verify_app_hash waits for CHECK_HEIGHT and compares its app hash with EXPECT_APP_HASH.
verify_app_hash() {
    local deadline=$((SECONDS + 600)) got
    echo "waiting for height $CHECK_HEIGHT"
    until [ "$(rpc_height)" -ge "$CHECK_HEIGHT" ]; do
        [ "$SECONDS" -lt "$deadline" ] || die "the node did not reach height $CHECK_HEIGHT"
        sleep 2
    done
    got=$(curl -sf -m 5 "$RPC/block?height=$CHECK_HEIGHT" | jq -r .result.block.header.app_hash)
    if [ "$(upper "$got")" != "$(upper "$EXPECT_APP_HASH")" ]; then
        die "app hash at height $CHECK_HEIGHT is $got, expected $EXPECT_APP_HASH"
    fi
    echo "app hash at height $CHECK_HEIGHT matches"
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
    require_signer_state
    check_signer_digest
    svc_start
    if [ -z "$REMOTE_SIGNER" ]; then
        change in_node rm -f "$SIGNER_SUM"
    fi
    if [ -z "$EXPECT_APP_HASH" ] || [ "$DRY_RUN" = 1 ]; then
        return 0
    fi
    if [ "$SETUP" = none ]; then
        echo "app hash not checked: start the node, then run verify"
    else
        verify_app_hash
    fi
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
    "mode_$MODE"
}

main "$@"
