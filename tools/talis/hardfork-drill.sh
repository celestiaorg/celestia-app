#!/usr/bin/env bash
# Helper for recovery drills on a talis network; see docs/hardfork/drills.md.
# Usage: EXP=<talis experiment dir> tools/talis/hardfork-drill.sh <command> [args]
#
# Nodes are talis validators: root, home /root/.celestia-app, node runs in the
# tmux session "app" (validator_init.sh -> celestia-appd start | tee /root/logs).
# NODE is a validator index (0..N-1) or "all".
#
#   ips                         list name and IP of every validator
#   on NODE CMD...              run CMD over ssh
#   push NODE LOCAL REMOTE      scp a file to the node(s)
#   pull IDX REMOTE LOCAL       scp a file back from one node
#   stop NODE                   kill the node process (tmux session app)
#   start NODE                  start celestia-appd in tmux session app
#   heights                     height, app hash and catching_up per node
#   procs                       whether celestia-appd runs on each node; a halt
#                               can make the node exit, not only stop advancing
#   apphash H                   app hash of block H+1 header on every node (state after H)
#   swap-bin NODE LOCAL_BIN     stop, replace /bin/celestia-appd, keep the old as .prev
#   fanout IDX REMOTE NODE      copy file REMOTE from validator IDX to the same
#                               path on NODE over the private network; upload a
#                               large binary once with push, then fan it out
#   recovery NODE ARGS...       push scripts/hardfork/recovery.sh and run it with
#                               --home /root/.celestia-app --no-service ARGS
#   export IDX ARGS...          push scripts/hardfork/export.sh and run it on one node
#   timer MSG                   append "<utc time> MSG" to $EXP/timeline.txt
set -euo pipefail

EXP=${EXP:?set EXP to the talis experiment dir (has config.json)}
KEY=${KEY:-$HOME/.ssh/id_rsa}
REPO=${REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
HOME_DIR=/root/.celestia-app
SSH=(ssh -n -i "$KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=10)
SCP=(scp -i "$KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR)

die() { echo "error: $*" >&2; exit 1; }

ips() { jq -r '.validators[] | "\(.name) \(.public_ip)"' "$EXP/config.json"; }

targets() {
    local all=()
    while IFS= read -r line; do all+=("$line"); done < <(jq -r '.validators[].public_ip' "$EXP/config.json")
    [ "${#all[@]}" -gt 0 ] || die "no validators in $EXP/config.json"
    if [ "$1" = all ]; then
        printf '%s\n' "${all[@]}"
    else
        [[ "$1" =~ ^[0-9]+$ ]] && [ "$1" -lt "${#all[@]}" ] || die "no validator $1"
        echo "${all[$1]}"
    fi
}

# resolve sets IPS to the targets of a node; it exits on an unknown node,
# which a loop over $(targets ...) would not.
resolve() { IPS=$(targets "$1"); }

# quote prints its arguments quoted for the remote shell.
quote() { printf ' %q' "$@"; }

on() {
    local node=$1 ip
    shift
    resolve "$node"
    for ip in $IPS; do
        echo "== $ip: $*" >&2
        "${SSH[@]}" "root@$ip" "$@"
    done
}

push() {
    local ip
    resolve "$1"
    for ip in $IPS; do
        "${SCP[@]}" "$2" "root@$ip:$3"
    done
}

pull() {
    resolve "$1"
    "${SCP[@]}" "root@$IPS:$2" "$3"
}

stop() {
    on "$1" "tmux kill-session -t app 2>/dev/null; pkill -TERM -x celestia-appd; for i in \$(seq 60); do pgrep -x celestia-appd >/dev/null || exit 0; sleep 1; done; echo 'still running' >&2; exit 1"
}

start() {
    on "$1" "tmux has-session -t app 2>/dev/null && { echo 'already running' >&2; exit 1; }; cd /root && tmux new-session -d -s app 'celestia-appd start --home $HOME_DIR 2>&1 | tee -a /root/logs'"
}

procs() {
    local ip
    resolve all
    for ip in $IPS; do
        printf '%s ' "$ip"
        "${SSH[@]}" "root@$ip" "pgrep -x celestia-appd >/dev/null && echo running || echo stopped"
    done
}

private_ip() { jq -r ".validators[$1].private_ip" "$EXP/config.json"; }

fanout() {
    local src=$1 file=$2 node=$3 srcip srcpub ip sum dir
    [[ "$file" =~ ^/[A-Za-z0-9._/-]+$ ]] || die "fanout takes an absolute path of letters, digits, '.', '_', '-' and '/'"
    resolve "$src"
    srcpub=$IPS
    srcip=$(private_ip "$src")
    resolve "$node"
    sum=$("${SSH[@]}" "root@$srcpub" "sha256sum $file" | cut -d' ' -f1)
    [[ "$sum" =~ ^[0-9a-f]{64}$ ]] || die "no sha256 of $file on validator $src"
    # The server serves a fresh directory with only a copy of the file, never
    # the file's own directory, which may hold keys. A server left by an
    # interrupted run is stopped first; pkill runs in its own ssh command, as
    # the bracket keeps the pattern from matching that shell but not a shell
    # that also starts the server. The server stops after 10 minutes even if
    # this script is interrupted. Each node checks the hash of what it got.
    "${SSH[@]}" "root@$srcpub" "pkill -f 'http[.]server 8099'; rm -rf /tmp/fanout.*; sleep 1" || true
    dir=$("${SSH[@]}" "root@$srcpub" "d=\$(mktemp -d /tmp/fanout.XXXXXX) && cp $file \$d/ && echo \$d")
    [[ "$dir" =~ ^/tmp/fanout\.[A-Za-z0-9]+$ ]] || die "could not stage $file on validator $src"
    "${SSH[@]}" "root@$srcpub" "cd $dir && (nohup timeout 600 python3 -m http.server 8099 --bind $srcip >/dev/null 2>&1 &); for i in \$(seq 10); do curl -sf -o /dev/null http://$srcip:8099/$(basename "$file") && exit 0; sleep 1; done; exit 1" ||
        die "file server on validator $src did not start"
    local failed=0
    for ip in $IPS; do
        [ "$ip" = "$srcpub" ] && continue
        echo "== $ip: $file" >&2
        "${SSH[@]}" "root@$ip" "curl -sf -o $file http://$srcip:8099/$(basename "$file") && echo '$sum  $file' | sha256sum -c --quiet -" || failed=1
    done
    "${SSH[@]}" "root@$srcpub" "pkill -f 'http[.]server 8099'; rm -rf $dir" || true
    [ "$failed" -eq 0 ] || die "fanout failed on some nodes"
}

heights() {
    local ip
    resolve all
    for ip in $IPS; do
        printf '%s ' "$ip"
        "${SSH[@]}" "root@$ip" "curl -s -m 5 localhost:26657/status | jq -c '.result.sync_info | {h: .latest_block_height, app_hash: .latest_app_hash, catching_up}' || echo down"
    done
}

apphash() {
    local ip
    resolve all
    for ip in $IPS; do
        printf '%s ' "$ip"
        "${SSH[@]}" "root@$ip" "curl -s -m 5 'localhost:26657/block?height=$(($1 + 1))' | jq -r '.result.block.header.app_hash // \"no block\"'"
    done
}

swap_bin() {
    stop "$1"
    push "$1" "$2" /root/celestia-appd.new
    on "$1" "cp /bin/celestia-appd /bin/celestia-appd.prev && install -m 0755 /root/celestia-appd.new /bin/celestia-appd && celestia-appd version"
}

recovery() {
    local node=$1
    shift
    push "$node" "$REPO/scripts/hardfork/recovery.sh" /root/recovery.sh
    on "$node" "bash /root/recovery.sh$(quote "$@") --home $HOME_DIR --no-service"
}

export_state() {
    local idx=$1
    shift
    push "$idx" "$REPO/scripts/hardfork/export.sh" /root/export.sh
    on "$idx" "bash /root/export.sh --home $HOME_DIR$(quote "$@")"
}

timer() { echo "$(date -u +%FT%TZ) $*" | tee -a "$EXP/timeline.txt"; }

cmd=${1:-}
[ -n "$cmd" ] || { sed -n '2,30p' "$0"; exit 1; }
shift
case "$cmd" in
ips | on | push | pull | stop | start | heights | procs | apphash | fanout | recovery | timer) "$cmd" "$@" ;;
swap-bin) swap_bin "$@" ;;
export) export_state "$@" ;;
*) die "unknown command $cmd" ;;
esac
