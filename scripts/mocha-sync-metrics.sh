#!/bin/sh
# Measures Mocha sync time. Usage: ./mocha-sync-metrics.sh [--no-metrics]

set -o errexit -o nounset

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WRITE_METRICS=true

for arg in "$@"; do
    case $arg in
        --no-metrics) WRITE_METRICS=false; shift ;;
        --help|-h) echo "Usage: $0 [--no-metrics]"; exit 0 ;;
    esac
done

. "${SCRIPT_DIR}/mocha.sh"

POLL_INTERVAL=5
SYNC_TIMEOUT=7200
LOCAL_RPC="http://localhost:26657"
METRICS_FILE="${CELESTIA_APP_HOME}/sync_metrics.prom"

[ "$WRITE_METRICS" = "true" ] && echo "metrics file: ${METRICS_FILE}" || echo "metrics file: disabled"

cleanup() { pkill -TERM celestia-appd 2>/dev/null || true; }
trap cleanup EXIT INT TERM

setup_mocha_sync

echo "Starting celestia-appd in background..."
START_TIME=$(date +%s)
celestia-appd start --force-no-bbr > ${CELESTIA_APP_HOME}/node.log 2>&1 &
NODE_PID=$!

for i in $(seq 1 60); do
    curl -s ${LOCAL_RPC}/status > /dev/null 2>&1 && break
    sleep 2
done
curl -s ${LOCAL_RPC}/status > /dev/null 2>&1 || { echo "ERROR: RPC unavailable"; exit 1; }

printf "\n=== Monitoring State Sync Progress ===\n"
STATE_SYNC_COMPLETE=false
STATE_SYNC_END_TIME=""
PREV_HEIGHT=0
STALL_COUNT=0
MAX_STALLS=24  # 24 * 5s(poll interval) = 2 minutes

elapsed=0
while [ $elapsed -lt $SYNC_TIMEOUT ]; do
    kill -0 $NODE_PID 2>/dev/null || { echo "ERROR: process died"; exit 1; }

    STATUS=$(curl -s ${LOCAL_RPC}/status || echo "{}")
    CATCHING_UP=$(echo "$STATUS" | jq -r '.result.sync_info.catching_up // "true"')
    CURRENT_HEIGHT=$(echo "$STATUS" | jq -r '.result.sync_info.latest_block_height // "0"')
    NETWORK_TIP=$(curl -s $RPC/block | jq -r '.result.block.header.height // "0"')
    BLOCKS_BEHIND=$((NETWORK_TIP - CURRENT_HEIGHT))
    [ $BLOCKS_BEHIND -lt 0 ] && BLOCKS_BEHIND=0

    # Detect stalled sync (only if not at tip and not at height 0)
    if [ "$CURRENT_HEIGHT" = "$PREV_HEIGHT" ] && [ "$CURRENT_HEIGHT" != "0" ] && [ "$BLOCKS_BEHIND" -gt "5" ]; then
        STALL_COUNT=$((STALL_COUNT + 1))
        if [ $STALL_COUNT -ge $MAX_STALLS ]; then
            NUM_PEERS=$(curl -s ${LOCAL_RPC}/net_info | jq -r '.result.n_peers // "0"')
            echo "ERROR: Sync stalled for 2 minutes at height $CURRENT_HEIGHT"
            echo "Peers connected: $NUM_PEERS (check logs: ${CELESTIA_APP_HOME}/node.log)"
            exit 1
        fi
        echo "[$(date +%T)] Height: $CURRENT_HEIGHT / $NETWORK_TIP (${BLOCKS_BEHIND} behind) | ⚠ STALLED ($STALL_COUNT/${MAX_STALLS})"
    else
        STALL_COUNT=0
        echo "[$(date +%T)] Height: $CURRENT_HEIGHT / $NETWORK_TIP (${BLOCKS_BEHIND} behind) | Catching up: $CATCHING_UP"
    fi
    PREV_HEIGHT=$CURRENT_HEIGHT

    if [ "$STATE_SYNC_COMPLETE" = "false" ] && [ "$CURRENT_HEIGHT" -ge "$BLOCK_HEIGHT" ]; then
        STATE_SYNC_END_TIME=$(date +%s)
        STATE_SYNC_DURATION=$((STATE_SYNC_END_TIME - START_TIME))
        printf "\n✓ State sync complete! Reached trust height %s (%ss)\n=== Monitoring Block Sync to Tip ===\n" "$BLOCK_HEIGHT" "$STATE_SYNC_DURATION"
        STATE_SYNC_COMPLETE=true
    fi

    if [ "$BLOCKS_BEHIND" -le "0" ]; then
        TOTAL_END_TIME=$(date +%s)
        TOTAL_DURATION=$((TOTAL_END_TIME - START_TIME))
        BLOCK_SYNC_DURATION=$((TOTAL_END_TIME - ${STATE_SYNC_END_TIME:-$START_TIME}))
        [ -z "$STATE_SYNC_END_TIME" ] && STATE_SYNC_DURATION=$TOTAL_DURATION

        printf "\n=========================================\n"
        printf "✓ Sync to tip complete!\n"
        printf "=========================================\n"
        printf "State sync duration:      %ss\n" "$STATE_SYNC_DURATION"
        printf "Block sync duration:      %ss\n" "$BLOCK_SYNC_DURATION"
        printf "Total sync duration:      %ss\n" "$TOTAL_DURATION"
        printf "Final height:             %s\n" "$CURRENT_HEIGHT"
        printf "Network tip:              %s\n" "$NETWORK_TIP"
        printf "Blocks behind:            %s\n" "$BLOCKS_BEHIND"
        printf "=========================================\n"

        if [ "$WRITE_METRICS" = "true" ]; then
            cat > "$METRICS_FILE" <<-EOF
			mocha_state_sync_duration_seconds{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $STATE_SYNC_DURATION
			mocha_block_sync_duration_seconds{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $BLOCK_SYNC_DURATION
			mocha_total_sync_duration_seconds{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $TOTAL_DURATION
			mocha_sync_final_height{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $CURRENT_HEIGHT
			mocha_sync_blocks_behind{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $BLOCKS_BEHIND
			mocha_sync_success{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} 1
			mocha_sync_timestamp{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $TOTAL_END_TIME
			EOF
            printf "\nMetrics: %s\n" "$METRICS_FILE"
            printf "Configure node_exporter: --collector.textfile.directory=%s\n" "$CELESTIA_APP_HOME"
        fi
        exit 0
    fi

    sleep $POLL_INTERVAL
    elapsed=$((elapsed + POLL_INTERVAL))
done

printf "\nERROR: Sync timeout (%ss)\n" "$SYNC_TIMEOUT"
[ "$WRITE_METRICS" = "true" ] && cat > "$METRICS_FILE" <<-EOF
	mocha_sync_success{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} 0
	mocha_sync_timeout{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} 1
	EOF
exit 1
