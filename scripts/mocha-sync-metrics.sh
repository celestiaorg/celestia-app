#!/bin/sh
# Measures Mocha sync time. Usage: ./mocha-sync-metrics.sh [--no-metrics] [--iterations N] [--cooldown S]

set -o errexit -o nounset

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WRITE_METRICS=true
ITERATIONS=1
COOLDOWN=30

while [ $# -gt 0 ]; do
	case $1 in
	--no-metrics)
		WRITE_METRICS=false
		shift
		;;
	--iterations | -n)
		ITERATIONS="$2"
		shift 2
		;;
	--cooldown | -c)
		COOLDOWN="$2"
		shift 2
		;;
	--help | -h)
		echo "Usage: $0 [--no-metrics] [--iterations N] [--cooldown S]"
		echo ""
		echo "Options:"
		echo "  --no-metrics           Disable writing metrics file"
		echo "  --iterations N, -n N   Run N sync iterations (default: 1)"
		echo "  --cooldown S, -c S     Cooldown seconds between runs (default: 30)"
		echo "  --help, -h             Show this help message"
		exit 0
		;;
	*)
		echo "Unknown option: $1"
		exit 1
		;;
	esac
done

. "${SCRIPT_DIR}/mocha.sh"

POLL_INTERVAL=5
SYNC_TIMEOUT=7200
LOCAL_RPC="http://localhost:26657"
METRICS_FILE="${CELESTIA_APP_HOME}/sync_metrics.prom"

# Statistics calculation functions
calculate_min() {
	min=$1
	shift
	for val in "$@"; do
		[ "$val" -lt "$min" ] && min=$val
	done
	echo "$min"
}

calculate_max() {
	max=$1
	shift
	for val in "$@"; do
		[ "$val" -gt "$max" ] && max=$val
	done
	echo "$max"
}

calculate_avg() {
	sum=0
	count=0
	for val in "$@"; do
		sum=$((sum + val))
		count=$((count + 1))
	done
	echo $((sum / count))
}

calculate_variance() {
	avg=$1
	shift
	sum_sq_diff=0
	count=0
	for val in "$@"; do
		diff=$((val - avg))
		sum_sq_diff=$((sum_sq_diff + diff * diff))
		count=$((count + 1))
	done
	echo $((sum_sq_diff / count))
}

# Run a single sync iteration
run_sync_iteration() {
	iteration_num=$1

	printf "\n"
	printf "=========================================\n"
	printf "ITERATION %d/%d\n" "$iteration_num" "$ITERATIONS"
	printf "=========================================\n"

	cleanup() { pkill -TERM celestia-appd 2>/dev/null || true; }
	trap cleanup EXIT INT TERM

	setup_mocha_sync

	echo "Starting celestia-appd in background..."
	START_TIME=$(date +%s)
	celestia-appd start --force-no-bbr >${CELESTIA_APP_HOME}/node.log 2>&1 &
	NODE_PID=$!

	for i in $(seq 1 60); do
		curl -s ${LOCAL_RPC}/status >/dev/null 2>&1 && break
		sleep 2
	done
	curl -s ${LOCAL_RPC}/status >/dev/null 2>&1 || {
		echo "ERROR: RPC unavailable"
		exit 1
	}

	printf "\n=== Monitoring State Sync Progress ===\n"
	STATE_SYNC_COMPLETE=false
	STATE_SYNC_END_TIME=""
	PREV_HEIGHT=0
	STALL_COUNT=0
	MAX_STALLS=24 # 24 * 5s(poll interval) = 2 minutes

	elapsed=0
	while [ $elapsed -lt $SYNC_TIMEOUT ]; do
		kill -0 $NODE_PID 2>/dev/null || {
			echo "ERROR: process died"
			exit 1
		}

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
			printf "✓ Iteration %d complete!\n" "$iteration_num"
			printf "=========================================\n"
			printf "State sync duration:      %ss\n" "$STATE_SYNC_DURATION"
			printf "Block sync duration:      %ss\n" "$BLOCK_SYNC_DURATION"
			printf "Total sync duration:      %ss\n" "$TOTAL_DURATION"
			printf "Final height:             %s\n" "$CURRENT_HEIGHT"
			printf "Network tip:              %s\n" "$NETWORK_TIP"
			printf "Blocks behind:            %s\n" "$BLOCKS_BEHIND"
			printf "=========================================\n"

			# Cleanup
			pkill -TERM celestia-appd 2>/dev/null || true
			trap - EXIT INT TERM

			# Return metrics as space-separated values
			echo "${STATE_SYNC_DURATION} ${BLOCK_SYNC_DURATION} ${TOTAL_DURATION}"
			return 0
		fi

		sleep $POLL_INTERVAL
		elapsed=$((elapsed + POLL_INTERVAL))
	done

	printf "\nERROR: Sync timeout (%ss)\n" "$SYNC_TIMEOUT"
	exit 1
}

# Main execution
echo "Starting sync metrics collection"
echo "Iterations: $ITERATIONS"
echo "Cooldown: ${COOLDOWN}s"
[ "$WRITE_METRICS" = "true" ] && echo "Metrics file: ${METRICS_FILE}" || echo "Metrics file: disabled"

# Arrays to store results
STATE_SYNC_DURATIONS=""
BLOCK_SYNC_DURATIONS=""
TOTAL_DURATIONS=""

# Run iterations
for i in $(seq 1 $ITERATIONS); do
	result=$(run_sync_iteration $i)

	# Parse results
	state_sync=$(echo "$result" | awk '{print $1}')
	block_sync=$(echo "$result" | awk '{print $2}')
	total=$(echo "$result" | awk '{print $3}')

	# Append to result lists
	STATE_SYNC_DURATIONS="${STATE_SYNC_DURATIONS} ${state_sync}"
	BLOCK_SYNC_DURATIONS="${BLOCK_SYNC_DURATIONS} ${block_sync}"
	TOTAL_DURATIONS="${TOTAL_DURATIONS} ${total}"

	# Cooldown between iterations (except after last one)
	if [ $i -lt $ITERATIONS ]; then
		printf "\nCooldown for %ss before next iteration...\n" "$COOLDOWN"
		sleep $COOLDOWN
	fi
done

# Calculate statistics
printf "\n\n"
printf "=========================================\n"
printf "FINAL STATISTICS (%d iterations)\n" "$ITERATIONS"
printf "=========================================\n"

if [ $ITERATIONS -eq 1 ]; then
	printf "State sync duration:      %ss\n" "$(echo $STATE_SYNC_DURATIONS | awk '{print $1}')"
	printf "Block sync duration:      %ss\n" "$(echo $BLOCK_SYNC_DURATIONS | awk '{print $1}')"
	printf "Total sync duration:      %ss\n" "$(echo $TOTAL_DURATIONS | awk '{print $1}')"
else
	# State sync stats
	state_min=$(calculate_min $STATE_SYNC_DURATIONS)
	state_max=$(calculate_max $STATE_SYNC_DURATIONS)
	state_avg=$(calculate_avg $STATE_SYNC_DURATIONS)
	state_var=$(calculate_variance $state_avg $STATE_SYNC_DURATIONS)

	# Block sync stats
	block_min=$(calculate_min $BLOCK_SYNC_DURATIONS)
	block_max=$(calculate_max $BLOCK_SYNC_DURATIONS)
	block_avg=$(calculate_avg $BLOCK_SYNC_DURATIONS)
	block_var=$(calculate_variance $block_avg $BLOCK_SYNC_DURATIONS)

	# Total sync stats
	total_min=$(calculate_min $TOTAL_DURATIONS)
	total_max=$(calculate_max $TOTAL_DURATIONS)
	total_avg=$(calculate_avg $TOTAL_DURATIONS)
	total_var=$(calculate_variance $total_avg $TOTAL_DURATIONS)

	printf "\nState Sync:\n"
	printf "  Min:       %ss\n" "$state_min"
	printf "  Max:       %ss\n" "$state_max"
	printf "  Average:   %ss\n" "$state_avg"
	printf "  Variance:  %s\n" "$state_var"

	printf "\nBlock Sync:\n"
	printf "  Min:       %ss\n" "$block_min"
	printf "  Max:       %ss\n" "$block_max"
	printf "  Average:   %ss\n" "$block_avg"
	printf "  Variance:  %s\n" "$block_var"

	printf "\nTotal Sync:\n"
	printf "  Min:       %ss\n" "$total_min"
	printf "  Max:       %ss\n" "$total_max"
	printf "  Average:   %ss\n" "$total_avg"
	printf "  Variance:  %s\n" "$total_var"

	printf "\nRaw data:\n"
	printf "  State sync: %s\n" "$STATE_SYNC_DURATIONS"
	printf "  Block sync: %s\n" "$BLOCK_SYNC_DURATIONS"
	printf "  Total:      %s\n" "$TOTAL_DURATIONS"
fi
printf "=========================================\n"

# Write metrics file
if [ "$WRITE_METRICS" = "true" ]; then
	if [ $ITERATIONS -eq 1 ]; then
		cat >"$METRICS_FILE" <<-EOF
			mocha_state_sync_duration_seconds{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $(echo $STATE_SYNC_DURATIONS | awk '{print $1}')
			mocha_block_sync_duration_seconds{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $(echo $BLOCK_SYNC_DURATIONS | awk '{print $1}')
			mocha_total_sync_duration_seconds{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $(echo $TOTAL_DURATIONS | awk '{print $1}')
			mocha_sync_success{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} 1
			mocha_sync_timestamp{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $(date +%s)
		EOF
	else
		cat >"$METRICS_FILE" <<-EOF
			mocha_state_sync_duration_seconds_min{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $state_min
			mocha_state_sync_duration_seconds_max{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $state_max
			mocha_state_sync_duration_seconds_avg{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $state_avg
			mocha_state_sync_duration_seconds_var{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $state_var
			mocha_block_sync_duration_seconds_min{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $block_min
			mocha_block_sync_duration_seconds_max{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $block_max
			mocha_block_sync_duration_seconds_avg{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $block_avg
			mocha_block_sync_duration_seconds_var{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $block_var
			mocha_total_sync_duration_seconds_min{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $total_min
			mocha_total_sync_duration_seconds_max{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $total_max
			mocha_total_sync_duration_seconds_avg{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $total_avg
			mocha_total_sync_duration_seconds_var{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $total_var
			mocha_sync_iterations{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $ITERATIONS
			mocha_sync_success{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} 1
			mocha_sync_timestamp{chain_id="$CHAIN_ID",version="$CELESTIA_APP_VERSION"} $(date +%s)
		EOF
	fi
	printf "\nMetrics written to: %s\n" "$METRICS_FILE"
fi

exit 0
