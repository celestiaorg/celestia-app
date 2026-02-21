#!/usr/bin/env bash
set -euo pipefail

# Mainnet TreeDB fast-sync forensics runner.
#
# Optional TreeDB trace knobs (all optional, disabled by default):
# - TREEDB_TRACE_PATH: enables TreeDB JSONL tracing when set (e.g. "$HOME/treedb-trace.jsonl").
# - TREEDB_TRACE_SUMMARY_PATH: JSON summary output path (default: "${TREEDB_TRACE_PATH}.summary.json").
# - TREEDB_TRACE_EVERY_N: trace sampling interval (default: 1 = every event).
# - TREEDB_TRACE_ANALYSIS_PATH: script-generated text diagnostics path
#   (default: "$LOG_DIR/diagnostics/treedb-trace-analysis.txt").
# - TREEDB_TRACE_ANALYSIS_TOP_N: top phases/iterators to include in diagnostics (default: 8).
# - TREEDB_TRACE_ANALYSIS_LONG_ITER_MS: long-lived iterator threshold in ms (default: 1000).
# - TREEDB_TRACE_ANALYSIS_SAMPLE_EVERY_N: sampling for diagnostics parser (default: 1).
# - TREEDB_TRACE_ANALYSIS_MAX_EVENTS: max sampled events parsed per report; 0 = unlimited (default: 200000).
# - TREEDB_TRACE_ANALYSIS_ON_WARN_STUCK: set to 1 to include trace report in periodic warn-stuck diagnostics
#   (default: 0; always included for fail/grace diagnostics when trace is enabled).
#
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

if ! command -v jq >/dev/null 2>&1;
then
  echo "jq is required for this script."
  exit 1
fi
if command -v rg >/dev/null 2>&1; then
  HAVE_RG=1
else
  HAVE_RG=0
fi

APPD_BIN="${CELESTIA_APPD_BIN:-${REPO_DIR}/build/celestia-appd}"
if [ ! -x "${APPD_BIN}" ];
then
  echo "celestia-appd not found; run 'make install-standalone' or set CELESTIA_APPD_BIN."
  exit 1
fi

CHAIN_ID="celestia"
RPC1="https://celestia.rpc.kjnodes.com"
RPC2="https://celestia-rpc.polkachu.com:443"
CURL_OPTS=(--max-time 10 --connect-timeout 5 --retry 3 --retry-delay 2)
LOCAL_CURL_OPTS=(--max-time 3 --connect-timeout 1)
LOCAL_RPC="http://127.0.0.1:36657"
P2P_LADDR="tcp://0.0.0.0:36656"
RPC_LADDR="tcp://127.0.0.1:36657"
PPROF_LADDR="localhost:6062"
DB_BACKEND="${DB_BACKEND:-treedb}"
APP_DB_BACKEND="${APP_DB_BACKEND:-${DB_BACKEND}}"
EXTERNAL_ADDRESS="${EXTERNAL_ADDRESS:-}"

# TreeDB trace capture (opt-in via TREEDB_TRACE_PATH).
TREEDB_TRACE_PATH="${TREEDB_TRACE_PATH:-}"
TREEDB_TRACE_SUMMARY_PATH="${TREEDB_TRACE_SUMMARY_PATH:-}"
TREEDB_TRACE_EVERY_N="${TREEDB_TRACE_EVERY_N:-1}"
TREEDB_TRACE_ANALYSIS_PATH="${TREEDB_TRACE_ANALYSIS_PATH:-}"
TREEDB_TRACE_ANALYSIS_TOP_N="${TREEDB_TRACE_ANALYSIS_TOP_N:-8}"
TREEDB_TRACE_ANALYSIS_LONG_ITER_MS="${TREEDB_TRACE_ANALYSIS_LONG_ITER_MS:-1000}"
TREEDB_TRACE_ANALYSIS_SAMPLE_EVERY_N="${TREEDB_TRACE_ANALYSIS_SAMPLE_EVERY_N:-1}"
TREEDB_TRACE_ANALYSIS_MAX_EVENTS="${TREEDB_TRACE_ANALYSIS_MAX_EVENTS:-200000}"
TREEDB_TRACE_ANALYSIS_ON_WARN_STUCK="${TREEDB_TRACE_ANALYSIS_ON_WARN_STUCK:-0}"

TS="$(date +%Y%m%d%H%M%S)"
HOME_DIR="${HOME}/.celestia-app-mainnet-${DB_BACKEND}-${TS}"
LOG_DIR="${HOME_DIR}/sync"
NODE_LOG="${LOG_DIR}/node.log"
TIME_LOG="${LOG_DIR}/sync-time.log"

POLL_INTERVAL_SECONDS="${POLL_INTERVAL_SECONDS:-10}"
WAIT_RPC_TIMEOUT_SECONDS="${WAIT_RPC_TIMEOUT_SECONDS:-180}"
NO_PROGRESS_WARN_SECONDS="${NO_PROGRESS_WARN_SECONDS:-60}"
NO_PROGRESS_FAIL_SECONDS="${NO_PROGRESS_FAIL_SECONDS:-600}"
STUCK_REPORT_INTERVAL_SECONDS="${STUCK_REPORT_INTERVAL_SECONDS:-30}"
MAX_LOCAL_RPC_FAILURES="${MAX_LOCAL_RPC_FAILURES:-6}"
MAX_REMOTE_RPC_FAILURES="${MAX_REMOTE_RPC_FAILURES:-12}"
LOG_ERROR_SCAN_LINES="${LOG_ERROR_SCAN_LINES:-300}"
NO_PROGRESS_HARD_FAIL_SECONDS="${NO_PROGRESS_HARD_FAIL_SECONDS:-3600}"
ACTIVE_RESTORE_CPU_THRESHOLD="${ACTIVE_RESTORE_CPU_THRESHOLD:-85}"
ACTIVE_RESTORE_GRACE_SECONDS="${ACTIVE_RESTORE_GRACE_SECONDS:-900}"
CAPTURE_PPROF_ON_STUCK="${CAPTURE_PPROF_ON_STUCK:-1}"
PPROF_SAMPLE_SECONDS="${PPROF_SAMPLE_SECONDS:-8}"
PPROF_HTTP_URL="http://${PPROF_LADDR}"

ERROR_PATTERNS='valuelog: corrupt record|state sync failed|state sync aborted|failed to restore snapshot|IAVL node import failed|IAVL commit failed|panic:|fatal error'

log_info() {
  echo "[$(date +%H:%M:%S)] INFO  $*"
}

log_warn() {
  echo "[$(date +%H:%M:%S)] WARN  $*"
}

log_error() {
  echo "[$(date +%H:%M:%S)] ERROR $*" >&2
}

print_recent_log_excerpt() {
  if [ -f "${NODE_LOG}" ]; then
    log_info "Node log: ${NODE_LOG}"
    log_info "Last 30 node-log lines:"
    tail -n 30 "${NODE_LOG}" 2>/dev/null || true
  fi
}

strip_ansi() {
  sed -E 's/\x1B\[[0-9;]*[mK]//g'
}

is_non_negative_int() {
  [[ "${1:-}" =~ ^[0-9]+$ ]]
}

is_positive_int() {
  [[ "${1:-}" =~ ^[1-9][0-9]*$ ]]
}

fetch_remote_latest_height() {
  local rpc status height
  for rpc in "${RPC1}" "${RPC2}"; do
    status="$(curl -fsSL "${CURL_OPTS[@]}" "${rpc}/status" 2>/dev/null || true)"
    if [ -z "${status}" ]; then
      continue
    fi
    height="$(echo "${status}" | jq -er '.result.sync_info.latest_block_height | tonumber' 2>/dev/null || true)"
    if is_non_negative_int "${height}"; then
      echo "${height}"
      return 0
    fi
  done
  return 1
}

fetch_trust_hash() {
  local trust_height="$1"
  local rpc block hash
  for rpc in "${RPC1}" "${RPC2}"; do
    block="$(curl -fsSL "${CURL_OPTS[@]}" "${rpc}/block?height=${trust_height}" 2>/dev/null || true)"
    if [ -z "${block}" ]; then
      continue
    fi
    hash="$(echo "${block}" | jq -er '.result.block_id.hash' 2>/dev/null || true)"
    if [[ "${hash}" =~ ^[A-Fa-f0-9]{64}$ ]]; then
      echo "${hash}"
      return 0
    fi
  done
  return 1
}

write_treedb_trace_report() {
  local report_file="$1"
  local context="${2:-post-run}"
  if [ -z "${TREEDB_TRACE_PATH}" ]; then
    return 0
  fi
  python3 - "${TREEDB_TRACE_PATH}" "${TREEDB_TRACE_SUMMARY_PATH}" "${report_file}" "${context}" "${TREEDB_TRACE_ANALYSIS_TOP_N}" "${TREEDB_TRACE_ANALYSIS_LONG_ITER_MS}" "${TREEDB_TRACE_ANALYSIS_SAMPLE_EVERY_N}" "${TREEDB_TRACE_ANALYSIS_MAX_EVENTS}" <<'PY'
import collections
import heapq
import json
import os
import sys
import time

trace_path = sys.argv[1]
summary_path = sys.argv[2]
report_path = sys.argv[3]
context = sys.argv[4]
top_n_raw = sys.argv[5]
long_iter_ms_raw = sys.argv[6]
sample_every_raw = sys.argv[7]
max_events_raw = sys.argv[8]


def parse_int(raw, default, minimum=0):
    try:
        value = int(raw)
    except Exception:
        return default
    if value < minimum:
        return default
    return value


top_n = parse_int(top_n_raw, 8, 1)
long_iter_ms = parse_int(long_iter_ms_raw, 1000, 0)
sample_every = parse_int(sample_every_raw, 1, 1)
max_events = parse_int(max_events_raw, 200000, 0)

phase_summaries = {}
summary_read_error = ""
if summary_path and os.path.isfile(summary_path):
    try:
        with open(summary_path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
            if isinstance(data, dict):
                phase_summaries = data
    except Exception as exc:
        summary_read_error = str(exc)


def count_value(entry, key):
    value = entry.get(key, 0)
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value)
    return 0


phase_rows = []
for phase, raw_counts in phase_summaries.items():
    if not isinstance(raw_counts, dict):
        continue
    gets = count_value(raw_counts, "Gets")
    has = count_value(raw_counts, "Has")
    iter_next = count_value(raw_counts, "IterNext")
    iter_create = count_value(raw_counts, "IterCreate")
    iter_close = count_value(raw_counts, "IterClose")
    iter_duration_nanos = count_value(raw_counts, "IterDurationNanos")
    read_ops = gets + has + iter_next
    iter_open_delta = iter_create - iter_close
    iter_duration_ms = iter_duration_nanos / 1_000_000.0
    iter_avg_close_ms = (iter_duration_ms / iter_close) if iter_close > 0 else 0.0
    phase_rows.append(
        {
            "phase": str(phase),
            "read_ops": read_ops,
            "gets": gets,
            "has": has,
            "iter_next": iter_next,
            "iter_create": iter_create,
            "iter_close": iter_close,
            "iter_open_delta": iter_open_delta,
            "iter_duration_ms": iter_duration_ms,
            "iter_avg_close_ms": iter_avg_close_ms,
        }
    )

phase_rows.sort(
    key=lambda row: (
        row["read_ops"],
        row["iter_open_delta"],
        row["iter_duration_ms"],
    ),
    reverse=True,
)

events_seen = 0
events_sampled = 0
json_parse_errors = 0
trace_read_error = ""
op_counts = collections.Counter()
trace_phase_reads = collections.Counter()
open_iters = {}
iter_create_events = 0
iter_close_events = 0
iter_close_without_create = 0
long_iter_count = 0
longest_iter_heap = []
latest_ts = 0


def append_long_iter(ms, iter_id, phase, kind, nexts):
    item = (int(ms), int(iter_id), str(phase), str(kind), int(nexts))
    if len(longest_iter_heap) < top_n:
        heapq.heappush(longest_iter_heap, item)
        return
    if item[0] > longest_iter_heap[0][0]:
        heapq.heapreplace(longest_iter_heap, item)


if trace_path and os.path.isfile(trace_path):
    try:
        with open(trace_path, "r", encoding="utf-8") as fh:
            for raw_line in fh:
                line = raw_line.strip()
                if not line:
                    continue
                events_seen += 1
                if sample_every > 1 and (events_seen % sample_every) != 0:
                    continue
                events_sampled += 1
                if max_events > 0 and events_sampled > max_events:
                    break
                try:
                    event = json.loads(line)
                except Exception:
                    json_parse_errors += 1
                    continue
                if not isinstance(event, dict):
                    continue

                op = str(event.get("op", "")).strip().lower()
                if not op:
                    continue
                phase = str(event.get("phase", "unknown"))
                op_counts[op] += 1

                ts = event.get("ts_unix_nano", 0)
                if isinstance(ts, (int, float)):
                    ts = int(ts)
                    if ts > latest_ts:
                        latest_ts = ts
                else:
                    ts = 0

                if op in ("get", "has"):
                    trace_phase_reads[phase] += 1

                if op == "iter_create":
                    iter_create_events += 1
                    iter_id = int(event.get("iter_id", 0) or 0)
                    open_iters[iter_id] = {
                        "ts": ts,
                        "phase": phase,
                        "kind": str(event.get("iter_kind", "")),
                    }
                    continue

                if op == "iter_close":
                    iter_close_events += 1
                    iter_id = int(event.get("iter_id", 0) or 0)
                    nexts = int(event.get("iter_nexts", 0) or 0)
                    trace_phase_reads[phase] += nexts

                    iter_ms = event.get("iter_ms", 0)
                    if isinstance(iter_ms, (int, float)):
                        dur_ms = int(iter_ms)
                    else:
                        dur_ms = 0
                    create_event = open_iters.pop(iter_id, None)
                    if create_event is None:
                        iter_close_without_create += 1
                    if dur_ms <= 0 and create_event is not None and ts > 0 and create_event.get("ts", 0) > 0:
                        dur_ms = max(0, int((ts - int(create_event["ts"])) / 1_000_000))
                    iter_kind = str(event.get("iter_kind", ""))
                    if not iter_kind and create_event is not None:
                        iter_kind = str(create_event.get("kind", ""))
                    if dur_ms >= long_iter_ms:
                        long_iter_count += 1
                    append_long_iter(dur_ms, iter_id, phase, iter_kind, nexts)
    except Exception as exc:
        trace_read_error = str(exc)

open_iter_rows = []
if open_iters:
    now_ns = latest_ts if latest_ts > 0 else time.time_ns()
    for iter_id, info in open_iters.items():
        created = int(info.get("ts", 0) or 0)
        age_ms = max(0, int((now_ns - created) / 1_000_000)) if created > 0 else 0
        open_iter_rows.append(
            (
                age_ms,
                int(iter_id),
                str(info.get("phase", "unknown")),
                str(info.get("kind", "")),
            )
        )
open_iter_rows.sort(reverse=True)
open_iter_rows = open_iter_rows[:top_n]

top_trace_phases = trace_phase_reads.most_common(top_n)
top_ops = op_counts.most_common(top_n)
longest_iters = sorted(longest_iter_heap, reverse=True)

with open(report_path, "w", encoding="utf-8") as out:
    out.write("TreeDB Trace Diagnostics\n")
    out.write(f"context={context}\n")
    out.write(f"generated_utc={time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}\n")
    out.write(f"trace_path={trace_path}\n")
    out.write(f"trace_exists={str(os.path.isfile(trace_path)).lower()}\n")
    out.write(f"summary_path={summary_path}\n")
    out.write(f"summary_exists={str(os.path.isfile(summary_path)).lower()}\n")
    out.write(f"sample_every_n={sample_every}\n")
    out.write(f"max_events={max_events}\n")
    out.write(f"events_seen={events_seen}\n")
    out.write(f"events_sampled={events_sampled}\n")
    out.write(f"json_parse_errors={json_parse_errors}\n")
    if trace_read_error:
        out.write(f"trace_read_error={trace_read_error}\n")
    if summary_read_error:
        out.write(f"summary_read_error={summary_read_error}\n")
    out.write("\n")

    out.write("iterator_lifecycle_from_trace:\n")
    out.write(f"iter_create_events={iter_create_events}\n")
    out.write(f"iter_close_events={iter_close_events}\n")
    out.write(f"iter_open_unclosed={len(open_iters)}\n")
    out.write(f"iter_close_without_create={iter_close_without_create}\n")
    out.write(f"long_iter_threshold_ms={long_iter_ms}\n")
    out.write(f"long_iter_close_events={long_iter_count}\n")
    out.write("\n")

    out.write("top_long_iter_close_events:\n")
    if longest_iters:
        for dur_ms, iter_id, phase, kind, nexts in longest_iters:
            out.write(
                f"- iter_id={iter_id} phase={phase} kind={kind or 'unknown'} "
                f"iter_ms={dur_ms} iter_nexts={nexts}\n"
            )
    else:
        out.write("- none\n")
    out.write("\n")

    out.write("oldest_open_iterators:\n")
    if open_iter_rows:
        for age_ms, iter_id, phase, kind in open_iter_rows:
            out.write(
                f"- iter_id={iter_id} phase={phase} kind={kind or 'unknown'} age_ms={age_ms}\n"
            )
    else:
        out.write("- none\n")
    out.write("\n")

    out.write("top_read_heavy_phases_from_summary:\n")
    if phase_rows:
        for row in phase_rows[:top_n]:
            out.write(
                f"- phase={row['phase']} read_ops={row['read_ops']} gets={row['gets']} has={row['has']} "
                f"iter_next={row['iter_next']} iter_create={row['iter_create']} iter_close={row['iter_close']} "
                f"iter_open_delta={row['iter_open_delta']} iter_avg_close_ms={row['iter_avg_close_ms']:.2f}\n"
            )
    else:
        out.write("- none\n")
    out.write("\n")

    out.write("top_read_heavy_phases_from_trace:\n")
    if top_trace_phases:
        for phase, count in top_trace_phases:
            out.write(f"- phase={phase} sampled_read_ops={count}\n")
    else:
        out.write("- none\n")
    out.write("\n")

    out.write("top_trace_ops:\n")
    if top_ops:
        for op, count in top_ops:
            out.write(f"- op={op} count={count}\n")
    else:
        out.write("- none\n")
PY
}

append_treedb_trace_report() {
  local parent_report="$1"
  local context="$2"
  local output_path="$3"
  if [ -z "${TREEDB_TRACE_PATH}" ]; then
    return 0
  fi
  if [ "${context}" = "warn-stuck" ] && [ "${TREEDB_TRACE_ANALYSIS_ON_WARN_STUCK}" != "1" ]; then
    return 0
  fi
  write_treedb_trace_report "${output_path}" "${context}" || true
  if [ -s "${output_path}" ]; then
    {
      echo
      echo "treedb_trace_report=${output_path}"
    } >> "${parent_report}"
    log_warn "TreeDB trace diagnostics captured: ${output_path}"
  fi
}

mkdir -p "${LOG_DIR}"
DIAG_DIR="${LOG_DIR}/diagnostics"
mkdir -p "${DIAG_DIR}"

if [ -n "${TREEDB_TRACE_PATH}" ]; then
  if ! is_positive_int "${TREEDB_TRACE_EVERY_N}"; then
    log_error "TREEDB_TRACE_EVERY_N must be a positive integer (got: ${TREEDB_TRACE_EVERY_N})."
    exit 1
  fi
  if ! is_positive_int "${TREEDB_TRACE_ANALYSIS_TOP_N}"; then
    log_error "TREEDB_TRACE_ANALYSIS_TOP_N must be a positive integer (got: ${TREEDB_TRACE_ANALYSIS_TOP_N})."
    exit 1
  fi
  if ! is_non_negative_int "${TREEDB_TRACE_ANALYSIS_LONG_ITER_MS}"; then
    log_error "TREEDB_TRACE_ANALYSIS_LONG_ITER_MS must be a non-negative integer (got: ${TREEDB_TRACE_ANALYSIS_LONG_ITER_MS})."
    exit 1
  fi
  if ! is_positive_int "${TREEDB_TRACE_ANALYSIS_SAMPLE_EVERY_N}"; then
    log_error "TREEDB_TRACE_ANALYSIS_SAMPLE_EVERY_N must be a positive integer (got: ${TREEDB_TRACE_ANALYSIS_SAMPLE_EVERY_N})."
    exit 1
  fi
  if ! is_non_negative_int "${TREEDB_TRACE_ANALYSIS_MAX_EVENTS}"; then
    log_error "TREEDB_TRACE_ANALYSIS_MAX_EVENTS must be a non-negative integer (got: ${TREEDB_TRACE_ANALYSIS_MAX_EVENTS})."
    exit 1
  fi
  if [ "${TREEDB_TRACE_ANALYSIS_ON_WARN_STUCK}" != "0" ] && [ "${TREEDB_TRACE_ANALYSIS_ON_WARN_STUCK}" != "1" ]; then
    log_error "TREEDB_TRACE_ANALYSIS_ON_WARN_STUCK must be 0 or 1 (got: ${TREEDB_TRACE_ANALYSIS_ON_WARN_STUCK})."
    exit 1
  fi
  if [ -z "${TREEDB_TRACE_SUMMARY_PATH}" ]; then
    TREEDB_TRACE_SUMMARY_PATH="${TREEDB_TRACE_PATH}.summary.json"
  fi
  if [ -z "${TREEDB_TRACE_ANALYSIS_PATH}" ]; then
    TREEDB_TRACE_ANALYSIS_PATH="${DIAG_DIR}/treedb-trace-analysis.txt"
  fi
  mkdir -p "$(dirname "${TREEDB_TRACE_PATH}")"
  mkdir -p "$(dirname "${TREEDB_TRACE_SUMMARY_PATH}")"
  mkdir -p "$(dirname "${TREEDB_TRACE_ANALYSIS_PATH}")"
  export TREEDB_TRACE_PATH TREEDB_TRACE_SUMMARY_PATH TREEDB_TRACE_EVERY_N
  log_info "TreeDB trace enabled (path=${TREEDB_TRACE_PATH}, summary=${TREEDB_TRACE_SUMMARY_PATH}, every_n=${TREEDB_TRACE_EVERY_N})."
fi

if ! is_positive_int "${MAX_REMOTE_RPC_FAILURES}"; then
  log_error "MAX_REMOTE_RPC_FAILURES must be a positive integer (got: ${MAX_REMOTE_RPC_FAILURES})."
  exit 1
fi

fallback_home=""
for dir in "${HOME}"/.celestia-app-mainnet-*; do
  if [ -f "${dir}/config/genesis.json" ]; then
    fallback_home="${dir}"
    break
  fi
done

fetch_or_copy() {
  local url="$1"
  local dest="$2"
  local fallback="$3"
  if ! curl -fsSL "${CURL_OPTS[@]}" "${url}" -o "${dest}"; then
    if [ -n "${fallback}" ] && [ -f "${fallback}" ]; then
      cp "${fallback}" "${dest}"
      return 0
    fi
    return 1
  fi
}

log_info "Using home: ${HOME_DIR}"
log_info "Logs: ${LOG_DIR}"

"${APPD_BIN}" init treedb-mainnet --chain-id "${CHAIN_ID}" --home "${HOME_DIR}" >/dev/null 2>&1

fetch_or_copy \
  https://raw.githubusercontent.com/celestiaorg/networks/master/celestia/genesis.json \
  "${HOME_DIR}/config/genesis.json" \
  "${fallback_home}/config/genesis.json"
fetch_or_copy \
  https://raw.githubusercontent.com/celestiaorg/networks/master/celestia/peers.txt \
  "${HOME_DIR}/config/peers.txt" \
  "${fallback_home}/config/peers.txt"
fetch_or_copy \
  https://raw.githubusercontent.com/celestiaorg/networks/master/celestia/seeds.txt \
  "${HOME_DIR}/config/seeds.txt" \
  "${fallback_home}/config/seeds.txt"

SEEDS="$(awk 'NF { print }' "${HOME_DIR}/config/seeds.txt" | paste -sd, -)"
PEERS="$(awk 'NF { print }' "${HOME_DIR}/config/peers.txt" | paste -sd, -)"

normalize_peer_csv() {
  local raw="${1:-}"
  PEER_CSV_RAW="${raw}" python3 - <<'PY'
import os
import re

raw = os.environ.get("PEER_CSV_RAW", "")
seen = set()
out = []
for token in (part.strip() for part in raw.split(",")):
    if not token or "@" not in token:
        continue
    node_id, addr = token.split("@", 1)
    node_id = node_id.strip()
    addr = addr.strip()
    if not node_id or not addr:
        continue

    host = ""
    port = ""
    if addr.startswith("["):
        m = re.match(r"^\[([^\]]+)\]:(\d+)$", addr)
        if not m:
            continue
        host, port = m.group(1), m.group(2)
        normalized_addr = f"[{host}]:{port}"
    else:
        if ":" not in addr:
            continue
        host, port = addr.rsplit(":", 1)
        if not port.isdigit() or not host:
            continue
        if ":" in host:
            normalized_addr = f"[{host}]:{port}"
        else:
            normalized_addr = f"{host}:{port}"

    normalized = f"{node_id}@{normalized_addr}"
    if normalized not in seen:
        seen.add(normalized)
        out.append(normalized)

print(",".join(out))
PY
}

SEEDS="$(normalize_peer_csv "${SEEDS}")"
PEERS="$(normalize_peer_csv "${PEERS}")"

NET_INFO_JSON="$(curl -fsSL "${CURL_OPTS[@]}" "${RPC1}/net_info" 2>/dev/null || curl -fsSL "${CURL_OPTS[@]}" "${RPC2}/net_info" 2>/dev/null || true)"
if [ -n "${NET_INFO_JSON}" ]; then
  NET_INFO_PEERS="$(echo "${NET_INFO_JSON}" | jq -r '[(.result.peers // [])[] | .node_info.id + "@" + .remote_ip + ":" + (.node_info.listen_addr | split(":") | last)] | join(",")' 2>/dev/null || true)"
  NET_INFO_PEERS="$(normalize_peer_csv "${NET_INFO_PEERS}")"
  if [ -n "${NET_INFO_PEERS}" ]; then
    PEERS="${NET_INFO_PEERS}"
  fi
fi

export HOME_DIR SEEDS PEERS P2P_LADDR RPC_LADDR PPROF_LADDR DB_BACKEND
python3 - <<'PY'
import os
import re
from pathlib import Path

cfg_path = Path(os.environ["HOME_DIR"]) / "config" / "config.toml"
data = cfg_path.read_text()
data, pprof_count = re.subn(
    r"(?m)^pprof_laddr\s*=.*$",
    f"pprof_laddr = \"{os.environ['PPROF_LADDR']}\"",
    data,
)
data, seeds_count = re.subn(
    r"(?m)^seeds\s*=.*$",
    f"seeds = \"{os.environ['SEEDS']}\"",
    data,
)
data, peers_count = re.subn(
    r"(?m)^persistent_peers\s*=.*$",
    f"persistent_peers = \"{os.environ['PEERS']}\"",
    data,
)
data, rpc_count = re.subn(
    r"(?m)^laddr\s*=\s*\"tcp://127.0.0.1:26657\"$",
    f"laddr = \"{os.environ['RPC_LADDR']}\"",
    data,
)
data, p2p_count = re.subn(
    r"(?m)^laddr\s*=\s*\"tcp://0.0.0.0:26656\"$",
    f"laddr = \"{os.environ['P2P_LADDR']}\"",
    data,
)
data, db_count = re.subn(
    r"(?m)^db_backend\s*=.*$",
    f"db_backend = \"{os.environ['DB_BACKEND']}\"",
    data,
)
if (
    pprof_count == 0
    or seeds_count == 0
    or peers_count == 0
    or rpc_count == 0
    or p2p_count == 0
    or db_count == 0
):
    raise SystemExit("Failed to update config.toml (ports/peers/seeds/pprof).")
cfg_path.write_text(data)
PY

export HOME_DIR APP_DB_BACKEND
python3 - <<'PY'
import os
import re
from pathlib import Path

app_path = Path(os.environ["HOME_DIR"]) / "config" / "app.toml"
data = app_path.read_text()
data, count = re.subn(
    r"(?m)^app-db-backend\s*=.*$",
    f"app-db-backend = \"{os.environ['APP_DB_BACKEND']}\"",
    data,
)
if count == 0:
    raise SystemExit("Failed to update app.toml (app-db-backend).")
app_path.write_text(data)
PY

if ! LATEST="$(fetch_remote_latest_height)"; then
  log_error "Failed to fetch latest block height from both remote RPCs."
  exit 1
fi
if ! is_non_negative_int "${LATEST}"; then
  log_error "Invalid latest block height from remote RPCs: '${LATEST}'."
  exit 1
fi
if [ "${LATEST}" -le 2000 ]; then
  log_error "Remote latest block height '${LATEST}' is too low to derive trust_height."
  exit 1
fi
TRUST_HEIGHT=$((LATEST - 2000))
if ! TRUST_HASH="$(fetch_trust_hash "${TRUST_HEIGHT}")"; then
  log_error "Failed to fetch trust hash for height ${TRUST_HEIGHT} from both remote RPCs."
  exit 1
fi

export HOME_DIR RPC1 RPC2 TRUST_HEIGHT TRUST_HASH
python3 - <<'PY'
import os
import re
from pathlib import Path

cfg_path = Path(os.environ["HOME_DIR"]) / "config" / "config.toml"
block = (
    "[statesync]\n"
    f"enable = true\n"
    f"rpc_servers = \"{os.environ['RPC1']},{os.environ['RPC2']}\"\n"
    f"trust_height = {os.environ['TRUST_HEIGHT']}\n"
    f"trust_hash = \"{os.environ['TRUST_HASH']}\"\n"
    "trust_period = \"168h\"\n\n"
)
data = cfg_path.read_text()
data, count = re.subn(r"(?ms)^\[statesync\][\s\S]*?(?=^\[blocksync\])", block, data, count=1)
if count == 0:
    raise SystemExit("Failed to update statesync config block.")
cfg_path.write_text(data)
PY

export HOME_DIR EXTERNAL_ADDRESS
python3 - <<'PY'
import os
import re
from pathlib import Path

cfg_path = Path(os.environ["HOME_DIR"]) / "config" / "config.toml"
data = cfg_path.read_text()

replacements = [
    (r"(?m)^max_open_connections\s*=.*$", "max_open_connections = 900", "max_open_connections"),
    (r"(?m)^max_num_inbound_peers\s*=.*$", "max_num_inbound_peers = 100", "max_num_inbound_peers"),
    (r"(?m)^max_num_outbound_peers\s*=.*$", "max_num_outbound_peers = 150", "max_num_outbound_peers"),
    (r"(?m)^upnp\s*=.*$", "upnp = true", "upnp"),
    (r"(?m)^handshake_timeout\s*=.*$", "handshake_timeout = \"20s\"", "handshake_timeout"),
    (r"(?m)^dial_timeout\s*=.*$", "dial_timeout = \"3s\"", "dial_timeout"),
    (r"(?m)^addr_book_strict\s*=.*$", "addr_book_strict = true", "addr_book_strict"),
    (r"(?m)^allow_duplicate_ip\s*=.*$", "allow_duplicate_ip = false", "allow_duplicate_ip"),
]
for pattern, value, name in replacements:
    data, count = re.subn(pattern, value, data)
    if count == 0:
        raise SystemExit(f"Failed to update config.toml ({name}).")

external_address = os.environ.get("EXTERNAL_ADDRESS", "")
if external_address:
    data, count = re.subn(
        r"(?m)^external_address\s*=.*$",
        f"external_address = \"{external_address}\"",
        data,
    )
    if count == 0:
        raise SystemExit("Failed to update config.toml (external_address).")

cfg_path.write_text(data)
PY

START_EPOCH="$(date +%s)"
START_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

safe_du_bytes() {
  local target="$1"
  if [ -e "${target}" ]; then
    if du -sb "${target}" >/dev/null 2>&1; then
      du -sb "${target}" 2>/dev/null | awk '{print $1}'
    else
      du -sk "${target}" 2>/dev/null | awk '{print $1 * 1024}'
    fi
  else
    echo 0
  fi
}

print_top_files_by_size() {
  local root="$1"
  local limit="${2:-20}"
  python3 - "${root}" "${limit}" <<'PY'
import heapq
import os
import sys

root = sys.argv[1]
try:
    limit = int(sys.argv[2])
except Exception:
    limit = 20
if limit < 1:
    limit = 1

heap = []
for dirpath, _, filenames in os.walk(root):
    for filename in filenames:
        path = os.path.join(dirpath, filename)
        try:
            size = os.path.getsize(path)
        except OSError:
            continue
        entry = (size, path)
        if len(heap) < limit:
            heapq.heappush(heap, entry)
        else:
            if size > heap[0][0]:
                heapq.heapreplace(heap, entry)

for size, path in sorted(heap, reverse=True):
    print(f"{size} {path}")
PY
}

START_HOME_BYTES="$(safe_du_bytes "${HOME_DIR}")"
START_DATA_BYTES="$(safe_du_bytes "${HOME_DIR}/data")"
START_APP_BYTES="$(safe_du_bytes "${HOME_DIR}/data/application.db")"
START_BLOCKSTORE_BYTES="$(safe_du_bytes "${HOME_DIR}/data/blockstore.db")"
MAX_RSS_KB=0
MAX_HWM_KB=0
{
  echo "start_utc=${START_TS}"
  echo "rpc1=${RPC1}"
  echo "rpc2=${RPC2}"
  echo "trust_height=${TRUST_HEIGHT}"
  echo "trust_hash=${TRUST_HASH}"
  echo "home=${HOME_DIR}"
  echo "db_backend=${DB_BACKEND}"
  echo "app_db_backend=${APP_DB_BACKEND}"
  echo "start_home_bytes=${START_HOME_BYTES}"
  echo "start_data_bytes=${START_DATA_BYTES}"
  echo "start_app_bytes=${START_APP_BYTES}"
  echo "start_blockstore_bytes=${START_BLOCKSTORE_BYTES}"
  if [ -n "${TREEDB_TRACE_PATH}" ]; then
    echo "treedb_trace_path=${TREEDB_TRACE_PATH}"
    echo "treedb_trace_summary_path=${TREEDB_TRACE_SUMMARY_PATH}"
    echo "treedb_trace_every_n=${TREEDB_TRACE_EVERY_N}"
    echo "treedb_trace_analysis_path=${TREEDB_TRACE_ANALYSIS_PATH}"
  else
    echo "treedb_trace_path=disabled"
  fi
} >> "${TIME_LOG}"

NODE_PID=""
cleanup_node() {
  if [ -n "${NODE_PID:-}" ] && kill -0 "${NODE_PID}" >/dev/null 2>&1; then
    kill -INT "${NODE_PID}" >/dev/null 2>&1 || true
    wait "${NODE_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup_node EXIT

fail_and_exit() {
  local reason="$1"
  log_error "${reason}"
  if [ -n "${TREEDB_TRACE_PATH}" ]; then
    local stamp
    stamp="$(date +%Y%m%d-%H%M%S)"
    local trace_report="${DIAG_DIR}/treedb-trace-failure-${stamp}.log"
    write_treedb_trace_report "${trace_report}" "failure" || true
    if [ -s "${trace_report}" ]; then
      log_warn "TreeDB trace diagnostics captured: ${trace_report}"
    fi
  fi
  print_recent_log_excerpt
  exit 1
}

has_recent_node_error() {
  if [ ! -f "${NODE_LOG}" ]; then
    return 1
  fi
  if [ "${HAVE_RG}" -eq 1 ]; then
    tail -n "${LOG_ERROR_SCAN_LINES}" "${NODE_LOG}" 2>/dev/null | rg -n -i -e "${ERROR_PATTERNS}" >/dev/null 2>&1
  else
    tail -n "${LOG_ERROR_SCAN_LINES}" "${NODE_LOG}" 2>/dev/null | grep -E -i -n "${ERROR_PATTERNS}" >/dev/null 2>&1
  fi
}

print_recent_error_matches() {
  if [ ! -f "${NODE_LOG}" ]; then
    return
  fi
  if [ "${HAVE_RG}" -eq 1 ]; then
    tail -n "${LOG_ERROR_SCAN_LINES}" "${NODE_LOG}" 2>/dev/null | rg -n -i -e "${ERROR_PATTERNS}" | tail -n 10 || true
  else
    tail -n "${LOG_ERROR_SCAN_LINES}" "${NODE_LOG}" 2>/dev/null | grep -E -i -n "${ERROR_PATTERNS}" | tail -n 10 || true
  fi
}

extract_sync_marker() {
  if [ ! -f "${NODE_LOG}" ]; then
    return
  fi
  tail -n "${LOG_ERROR_SCAN_LINES}" "${NODE_LOG}" \
    | grep -E "Applied snapshot chunk to ABCI app|Fetching snapshot chunk|Snapshot accepted, restoring|Offering snapshot to ABCI app|Starting state sync|Discovered new snapshot|Upgrading IAVL storage|executed block|finalizing commit of block|committed state" \
    | tail -n 1 \
    | strip_ansi || true
}

sync_stage_from_marker() {
  local marker="${1:-}"
  if [ -z "${marker}" ]; then
    echo "unknown"
    return
  fi
  if [[ "${marker}" == *"Applied snapshot chunk to ABCI app"* ]]; then
    local chunk="?"
    local total="?"
    local height="?"
    if [[ "${marker}" =~ chunk=([0-9]+) ]]; then chunk="${BASH_REMATCH[1]}"; fi
    if [[ "${marker}" =~ total=([0-9]+) ]]; then total="${BASH_REMATCH[1]}"; fi
    if [[ "${marker}" =~ height=([0-9]+) ]]; then height="${BASH_REMATCH[1]}"; fi
    echo "statesync_apply chunk=${chunk}/${total} snapshot_height=${height}"
    return
  fi
  if [[ "${marker}" == *"Fetching snapshot chunk"* ]]; then
    local chunk="?"
    local total="?"
    local height="?"
    if [[ "${marker}" =~ chunk=([0-9]+) ]]; then chunk="${BASH_REMATCH[1]}"; fi
    if [[ "${marker}" =~ total=([0-9]+) ]]; then total="${BASH_REMATCH[1]}"; fi
    if [[ "${marker}" =~ height=([0-9]+) ]]; then height="${BASH_REMATCH[1]}"; fi
    echo "statesync_fetch chunk=${chunk}/${total} snapshot_height=${height}"
    return
  fi
  if [[ "${marker}" == *"Snapshot accepted, restoring"* ]]; then
    echo "statesync_restore"
    return
  fi
  if [[ "${marker}" == *"Offering snapshot to ABCI app"* ]]; then
    echo "statesync_offer_snapshot"
    return
  fi
  if [[ "${marker}" == *"Starting state sync"* ]]; then
    echo "statesync_start"
    return
  fi
  if [[ "${marker}" == *"Discovered new snapshot"* ]]; then
    local height="?"
    if [[ "${marker}" =~ height=([0-9]+) ]]; then height="${BASH_REMATCH[1]}"; fi
    echo "statesync_discover height=${height}"
    return
  fi
  if [[ "${marker}" == *"Upgrading IAVL storage"* ]]; then
    echo "iavl_enable_fast_storage"
    return
  fi
  if [[ "${marker}" == *"executed block"* ]]; then
    echo "consensus_execute_block"
    return
  fi
  if [[ "${marker}" == *"finalizing commit of block"* ]]; then
    echo "consensus_finalize_commit"
    return
  fi
  if [[ "${marker}" == *"committed state"* ]]; then
    echo "consensus_committed_state"
    return
  fi
  echo "${marker}"
}

capture_stuck_diagnostics() {
  local reason="$1"
  local stalled_for="$2"
  local local_height="$3"
  local remote_height="$4"
  local lag="$5"
  local catching_up="$6"
  local node_cpu="$7"
  local rss_kb="$8"
  local with_pprof="${9:-0}"

  local stamp
  stamp="$(date +%Y%m%d-%H%M%S)"
  local report_file="${DIAG_DIR}/stuck-${stamp}.log"
  local statesync_file="${DIAG_DIR}/stuck-statesync-${stamp}.log"
  local goroutine_file="${DIAG_DIR}/stuck-goroutines-${stamp}.txt"
  local cpu_profile_file="${DIAG_DIR}/stuck-cpu-${stamp}.pb.gz"
  local cpu_top_file="${DIAG_DIR}/stuck-cpu-top-${stamp}.txt"
  local trace_report_file="${DIAG_DIR}/treedb-trace-${reason}-${stamp}.log"
  local stage_summary
  stage_summary="$(sync_stage_from_marker "${LAST_SYNC_MARKER}")"

  {
    echo "timestamp_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "reason=${reason}"
    echo "stalled_for_seconds=${stalled_for}"
    echo "local_height=${local_height}"
    echo "remote_height=${remote_height}"
    echo "lag=${lag}"
    echo "catching_up=${catching_up}"
    echo "node_pid=${NODE_PID}"
    echo "node_cpu_percent=${node_cpu:-n/a}"
    echo "node_rss_kb=${rss_kb:-n/a}"
    echo "sync_stage=${stage_summary}"
    echo "last_sync_marker=${LAST_SYNC_MARKER:-<none>}"
    echo "local_rpc=${LOCAL_RPC}"
    echo "pprof_url=${PPROF_HTTP_URL}"
    echo "node_log=${NODE_LOG}"
    echo "home=${HOME_DIR}"
    echo
    echo "process:"
    ps -p "${NODE_PID}" -o pid,ppid,%cpu,%mem,etime,rss,vsz,stat,cmd 2>/dev/null || true
    echo
    echo "threads (top CPU):"
    ps -L -p "${NODE_PID}" -o tid,psr,pcpu,stat,comm --sort=-pcpu 2>/dev/null | head -n 20 || true
    echo
    echo "local_status:"
    curl -fsSL "${LOCAL_CURL_OPTS[@]}" "${LOCAL_RPC}/status" 2>/dev/null | jq '.result.sync_info + {node_id: .result.node_info.id}' 2>/dev/null || true
    echo
    echo "local_net_info:"
    curl -fsSL "${LOCAL_CURL_OPTS[@]}" "${LOCAL_RPC}/net_info" 2>/dev/null | jq '{listening: .result.listening, n_peers: .result.n_peers}' 2>/dev/null || true
    echo
    echo "storage_bytes:"
    du -sb "${HOME_DIR}/data/application.db" "${HOME_DIR}/data/state.db" "${HOME_DIR}/data/snapshots" "${HOME_DIR}/data/blockstore.db" 2>/dev/null || true
    echo
    echo "iostat_pidstat:"
    if command -v pidstat >/dev/null 2>&1; then
      pidstat -d -p "${NODE_PID}" 1 3 2>/dev/null || true
    else
      echo "pidstat unavailable"
    fi
  } > "${report_file}"

  tail -n "${LOG_ERROR_SCAN_LINES}" "${NODE_LOG}" 2>/dev/null \
    | strip_ansi \
    | grep -E "Applied snapshot chunk to ABCI app|Fetching snapshot chunk|Snapshot accepted, restoring|Offering snapshot to ABCI app|Starting state sync|Discovered new snapshot|state sync|statesync|executed block|finalizing commit of block|committed state|Upgrading IAVL storage" \
    | tail -n 120 > "${statesync_file}" || true

  {
    echo
    echo "statesync_markers_file=${statesync_file}"
  } >> "${report_file}"

  if [ "${with_pprof}" = "1" ] && [ "${CAPTURE_PPROF_ON_STUCK}" = "1" ]; then
    if curl -fsS --max-time 5 "${PPROF_HTTP_URL}/debug/pprof/" >/dev/null 2>&1; then
      curl -fsS --max-time 10 "${PPROF_HTTP_URL}/debug/pprof/goroutine?debug=1" > "${goroutine_file}" 2>/dev/null || true
      if [ -s "${goroutine_file}" ]; then
        echo "pprof_goroutine_file=${goroutine_file}" >> "${report_file}"
      fi
      curl -fsS --max-time $((PPROF_SAMPLE_SECONDS + 8)) "${PPROF_HTTP_URL}/debug/pprof/profile?seconds=${PPROF_SAMPLE_SECONDS}" > "${cpu_profile_file}" 2>/dev/null || true
      if [ -s "${cpu_profile_file}" ] && command -v go >/dev/null 2>&1; then
        go tool pprof -top "${APPD_BIN}" "${cpu_profile_file}" > "${cpu_top_file}" 2>/dev/null || true
        if [ -s "${cpu_top_file}" ]; then
          echo "pprof_cpu_profile_file=${cpu_profile_file}" >> "${report_file}"
          echo "pprof_cpu_top_file=${cpu_top_file}" >> "${report_file}"
        fi
      fi
    else
      echo "pprof_unavailable=true" >> "${report_file}"
    fi
  fi

  append_treedb_trace_report "${report_file}" "${reason}" "${trace_report_file}"

  log_warn "Diagnostics captured: ${report_file}"
}

is_active_restore() {
  local node_cpu="$1"
  local marker="$2"
  if [ -z "${node_cpu}" ]; then
    return 1
  fi
  if ! awk -v c="${node_cpu}" -v t="${ACTIVE_RESTORE_CPU_THRESHOLD}" 'BEGIN { exit ((c + 0) >= (t + 0)) ? 0 : 1 }'; then
    return 1
  fi
  [[ "${marker}" == *"snapshot chunk"* ]] && return 0
  [[ "${marker}" == *"Snapshot accepted, restoring"* ]] && return 0
  [[ "${marker}" == *"Upgrading IAVL storage"* ]] && return 0
  return 1
}

log_info "Starting node..."
"${APPD_BIN}" start --home "${HOME_DIR}" --force-no-bbr >"${NODE_LOG}" 2>&1 &
NODE_PID=$!

log_info "Waiting for local RPC (${LOCAL_RPC})..."
RPC_WAIT_START="$(date +%s)"
LAST_WAIT_REPORT=0
until curl -fsSL "${LOCAL_CURL_OPTS[@]}" "${LOCAL_RPC}/status" >/dev/null 2>&1; do
  if ! kill -0 "${NODE_PID}" >/dev/null 2>&1; then
    fail_and_exit "Node exited before local RPC became ready."
  fi
  NOW_EPOCH="$(date +%s)"
  WAIT_ELAPSED=$((NOW_EPOCH-RPC_WAIT_START))
  if [ "${WAIT_ELAPSED}" -ge "${WAIT_RPC_TIMEOUT_SECONDS}" ]; then
    fail_and_exit "Local RPC was not ready after ${WAIT_RPC_TIMEOUT_SECONDS}s."
  fi
  if [ "${LAST_WAIT_REPORT}" -eq 0 ] || [ $((NOW_EPOCH - LAST_WAIT_REPORT)) -ge 20 ]; then
    log_info "Still waiting for local RPC (${WAIT_ELAPSED}s elapsed)..."
    LAST_WAIT_REPORT="${NOW_EPOCH}"
  fi
  sleep 2
done
log_info "Local RPC is ready."

LOCAL_HEIGHT=0
REMOTE_HEIGHT=0
CATCHING_UP=true
PREV_LOCAL_HEIGHT=-1
START_LOCAL_HEIGHT=0
PROGRESS_EPOCH="$(date +%s)"
LAST_STUCK_REPORT_EPOCH=0
LAST_SYNC_MARKER=""
LOCAL_RPC_FAILURES=0
REMOTE_RPC_FAILURES=0
SYNC_COMPLETE=0
ACTIVE_RESTORE_GRACE_USED=0

log_info "Monitoring sync progress..."
while true; do
  if ! kill -0 "${NODE_PID}" >/dev/null 2>&1; then
    fail_and_exit "Node process exited while syncing."
  fi

  if has_recent_node_error; then
    log_error "Detected fatal node error in recent logs:"
    print_recent_error_matches
    fail_and_exit "Sync aborted by node error."
  fi

  LOCAL_STATUS="$(curl -fsSL "${LOCAL_CURL_OPTS[@]}" "${LOCAL_RPC}/status" 2>/dev/null || true)"
  if [ -z "${LOCAL_STATUS}" ]; then
    LOCAL_RPC_FAILURES=$((LOCAL_RPC_FAILURES + 1))
    if [ "${LOCAL_RPC_FAILURES}" -ge "${MAX_LOCAL_RPC_FAILURES}" ]; then
      fail_and_exit "Local RPC unavailable for ${LOCAL_RPC_FAILURES} consecutive checks."
    fi
    log_warn "Local RPC unavailable (${LOCAL_RPC_FAILURES}/${MAX_LOCAL_RPC_FAILURES}); retrying..."
    sleep "${POLL_INTERVAL_SECONDS}"
    continue
  fi
  LOCAL_RPC_FAILURES=0

  LOCAL_HEIGHT="$(echo "${LOCAL_STATUS}" | jq -er '.result.sync_info.latest_block_height | tonumber' 2>/dev/null || true)"
  CATCHING_UP="$(echo "${LOCAL_STATUS}" | jq -er '.result.sync_info.catching_up | tostring' 2>/dev/null || true)"
  if [ -z "${LOCAL_HEIGHT}" ] || [ -z "${CATCHING_UP}" ]; then
    fail_and_exit "Failed to parse local RPC sync status."
  fi
  if [ "${PREV_LOCAL_HEIGHT}" -lt 0 ]; then
    PREV_LOCAL_HEIGHT="${LOCAL_HEIGHT}"
    START_LOCAL_HEIGHT="${LOCAL_HEIGHT}"
    PROGRESS_EPOCH="$(date +%s)"
  fi

  REMOTE_STATUS="$(curl -fsSL "${CURL_OPTS[@]}" "${RPC1}/status" 2>/dev/null || curl -fsSL "${CURL_OPTS[@]}" "${RPC2}/status" 2>/dev/null || true)"
  if [ -z "${REMOTE_STATUS}" ]; then
    REMOTE_RPC_FAILURES=$((REMOTE_RPC_FAILURES + 1))
    if [ "${REMOTE_RPC_FAILURES}" -ge "${MAX_REMOTE_RPC_FAILURES}" ]; then
      fail_and_exit "Remote RPC unavailable for ${REMOTE_RPC_FAILURES} consecutive checks."
    fi
    log_warn "Remote RPC unavailable (${REMOTE_RPC_FAILURES}/${MAX_REMOTE_RPC_FAILURES}); retrying..."
    sleep "${POLL_INTERVAL_SECONDS}"
    continue
  fi
  REMOTE_HEIGHT="$(echo "${REMOTE_STATUS}" | jq -er '.result.sync_info.latest_block_height | tonumber' 2>/dev/null || true)"
  if [ -z "${REMOTE_HEIGHT}" ]; then
    REMOTE_RPC_FAILURES=$((REMOTE_RPC_FAILURES + 1))
    if [ "${REMOTE_RPC_FAILURES}" -ge "${MAX_REMOTE_RPC_FAILURES}" ]; then
      fail_and_exit "Failed to parse remote RPC height for ${REMOTE_RPC_FAILURES} consecutive checks."
    fi
    log_warn "Failed to parse remote RPC height (${REMOTE_RPC_FAILURES}/${MAX_REMOTE_RPC_FAILURES}); retrying..."
    sleep "${POLL_INTERVAL_SECONDS}"
    continue
  fi
  REMOTE_RPC_FAILURES=0
  REMOTE_TARGET=$((REMOTE_HEIGHT - 2))
  if [ "${REMOTE_TARGET}" -lt 0 ]; then
    REMOTE_TARGET=0
  fi

  NOW_EPOCH="$(date +%s)"

  RSS_KB=""
  NODE_CPU=""
  if command -v ps >/dev/null 2>&1; then
    RSS_KB="$(ps -o rss= -p "${NODE_PID}" 2>/dev/null | awk '{print $1}' || true)"
    NODE_CPU="$(ps -o %cpu= -p "${NODE_PID}" 2>/dev/null | awk '{print $1}' || true)"
  fi
  if [ -z "${RSS_KB}" ] && [ -r "/proc/${NODE_PID}/status" ]; then
    RSS_KB="$(awk '/VmRSS:/ {print $2}' "/proc/${NODE_PID}/status" 2>/dev/null || true)"
  fi
  if [ -r "/proc/${NODE_PID}/status" ]; then
    HWM_KB="$(awk '/VmHWM:/ {print $2}' "/proc/${NODE_PID}/status" 2>/dev/null || true)"
  else
    HWM_KB=""
  fi
  if [ -n "${RSS_KB}" ] && [ "${RSS_KB}" -gt "${MAX_RSS_KB}" ]; then
    MAX_RSS_KB="${RSS_KB}"
  fi
  if [ -n "${HWM_KB}" ] && [ "${HWM_KB}" -gt "${MAX_HWM_KB}" ]; then
    MAX_HWM_KB="${HWM_KB}"
  fi

  LAG=$((REMOTE_HEIGHT - LOCAL_HEIGHT))
  if [ "${LAG}" -lt 0 ]; then
    LAG=0
  fi

  if [ "${LOCAL_HEIGHT}" -gt "${PREV_LOCAL_HEIGHT}" ]; then
    DELTA=$((LOCAL_HEIGHT - PREV_LOCAL_HEIGHT))
    TOTAL_DELTA=$((LOCAL_HEIGHT - START_LOCAL_HEIGHT))
    ELAPSED=$((NOW_EPOCH - START_EPOCH))
    if [ "${ELAPSED}" -le 0 ]; then
      ELAPSED=1
    fi
    RATE="$(awk "BEGIN { printf \"%.2f\", ${TOTAL_DELTA}/${ELAPSED} }")"
    log_info "Progress: local=${LOCAL_HEIGHT} remote=${REMOTE_HEIGHT} lag=${LAG} catching_up=${CATCHING_UP} (+${DELTA}, avg=${RATE} blk/s, cpu=${NODE_CPU:-n/a}%, rss=${RSS_KB:-n/a}k)"
    PREV_LOCAL_HEIGHT="${LOCAL_HEIGHT}"
    PROGRESS_EPOCH="${NOW_EPOCH}"
    LAST_STUCK_REPORT_EPOCH=0
  fi

  SYNC_MARKER="$(extract_sync_marker)"
  if [ -n "${SYNC_MARKER}" ] && [ "${SYNC_MARKER}" != "${LAST_SYNC_MARKER}" ]; then
    LAST_SYNC_MARKER="${SYNC_MARKER}"
    PROGRESS_EPOCH="${NOW_EPOCH}"
    if [[ "${SYNC_MARKER}" =~ chunk=([0-9]+) ]]; then
      CHUNK_NUM="${BASH_REMATCH[1]}"
      CHUNK_TOTAL="?"
      SNAPSHOT_HEIGHT="?"
      if [[ "${SYNC_MARKER}" =~ total=([0-9]+) ]]; then
        CHUNK_TOTAL="${BASH_REMATCH[1]}"
      fi
      if [[ "${SYNC_MARKER}" =~ height=([0-9]+) ]]; then
        SNAPSHOT_HEIGHT="${BASH_REMATCH[1]}"
      fi
      log_info "State-sync activity: snapshot_height=${SNAPSHOT_HEIGHT} chunk=${CHUNK_NUM}/${CHUNK_TOTAL} local=${LOCAL_HEIGHT}"
    fi
  fi

  STALLED_FOR=$((NOW_EPOCH - PROGRESS_EPOCH))
  if [ "${STALLED_FOR}" -ge "${NO_PROGRESS_WARN_SECONDS}" ]; then
    if [ "${LAST_STUCK_REPORT_EPOCH}" -eq 0 ] || [ $((NOW_EPOCH - LAST_STUCK_REPORT_EPOCH)) -ge "${STUCK_REPORT_INTERVAL_SECONDS}" ]; then
      STAGE_SUMMARY="$(sync_stage_from_marker "${LAST_SYNC_MARKER}")"
      log_warn "Stuck: no sync progress for ${STALLED_FOR}s (local=${LOCAL_HEIGHT}, remote=${REMOTE_HEIGHT}, lag=${LAG}, catching_up=${CATCHING_UP}, stage=${STAGE_SUMMARY}, cpu=${NODE_CPU:-n/a}%, rss=${RSS_KB:-n/a}k)"
      capture_stuck_diagnostics "warn-stuck" "${STALLED_FOR}" "${LOCAL_HEIGHT}" "${REMOTE_HEIGHT}" "${LAG}" "${CATCHING_UP}" "${NODE_CPU:-}" "${RSS_KB:-}" 0
      LAST_STUCK_REPORT_EPOCH="${NOW_EPOCH}"
    fi
  fi
  if [ "${STALLED_FOR}" -ge "${NO_PROGRESS_FAIL_SECONDS}" ]; then
    if [ "${ACTIVE_RESTORE_GRACE_USED}" -eq 0 ] && is_active_restore "${NODE_CPU:-}" "${LAST_SYNC_MARKER:-}"; then
      ACTIVE_RESTORE_GRACE_USED=1
      STAGE_SUMMARY="$(sync_stage_from_marker "${LAST_SYNC_MARKER}")"
      log_warn "No height progress for ${STALLED_FOR}s but node appears CPU-bound in ${STAGE_SUMMARY}; granting one-time ${ACTIVE_RESTORE_GRACE_SECONDS}s grace."
      capture_stuck_diagnostics "grace-active-restore" "${STALLED_FOR}" "${LOCAL_HEIGHT}" "${REMOTE_HEIGHT}" "${LAG}" "${CATCHING_UP}" "${NODE_CPU:-}" "${RSS_KB:-}" 1
      PROGRESS_EPOCH=$((NOW_EPOCH - NO_PROGRESS_FAIL_SECONDS + ACTIVE_RESTORE_GRACE_SECONDS))
      sleep "${POLL_INTERVAL_SECONDS}"
      continue
    fi
    capture_stuck_diagnostics "fail-no-progress" "${STALLED_FOR}" "${LOCAL_HEIGHT}" "${REMOTE_HEIGHT}" "${LAG}" "${CATCHING_UP}" "${NODE_CPU:-}" "${RSS_KB:-}" 1
    if [ "${STALLED_FOR}" -lt "${NO_PROGRESS_HARD_FAIL_SECONDS}" ] && is_active_restore "${NODE_CPU:-}" "${LAST_SYNC_MARKER:-}"; then
      STAGE_SUMMARY="$(sync_stage_from_marker "${LAST_SYNC_MARKER}")"
      log_warn "Still CPU-bound in ${STAGE_SUMMARY}; delaying hard fail until ${NO_PROGRESS_HARD_FAIL_SECONDS}s."
      sleep "${POLL_INTERVAL_SECONDS}"
      continue
    fi
    fail_and_exit "No sync progress for ${STALLED_FOR}s (treating as stuck)."
  fi

  if [ "${CATCHING_UP}" = "false" ] && [ "${LOCAL_HEIGHT}" -ge "${REMOTE_TARGET}" ]; then
    SYNC_COMPLETE=1
    break
  fi
  sleep "${POLL_INTERVAL_SECONDS}"
done

if [ "${SYNC_COMPLETE}" -ne 1 ]; then
  fail_and_exit "Sync monitor exited without success."
fi

END_EPOCH="$(date +%s)"
END_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
DURATION=$((END_EPOCH-START_EPOCH))
END_HOME_BYTES="$(safe_du_bytes "${HOME_DIR}")"
END_DATA_BYTES="$(safe_du_bytes "${HOME_DIR}/data")"
END_APP_BYTES="$(safe_du_bytes "${HOME_DIR}/data/application.db")"
END_BLOCKSTORE_BYTES="$(safe_du_bytes "${HOME_DIR}/data/blockstore.db")"

{
  echo "end_utc=${END_TS}"
  echo "duration_seconds=${DURATION}"
  echo "final_local_height=${LOCAL_HEIGHT}"
  echo "final_remote_height=${REMOTE_HEIGHT}"
  echo "max_rss_kb=${MAX_RSS_KB}"
  echo "max_hwm_kb=${MAX_HWM_KB}"
  echo "end_home_bytes=${END_HOME_BYTES}"
  echo "end_data_bytes=${END_DATA_BYTES}"
  echo "end_app_bytes=${END_APP_BYTES}"
  echo "end_blockstore_bytes=${END_BLOCKSTORE_BYTES}"
  echo "---"
} >> "${TIME_LOG}"

log_info "Sync complete: local=${LOCAL_HEIGHT} remote=${REMOTE_HEIGHT}. Stopping node..."
SHUTDOWN_START_EPOCH="$(date +%s)"
kill -INT "${NODE_PID}" >/dev/null 2>&1 || true
wait "${NODE_PID}" >/dev/null 2>&1 || true
SHUTDOWN_END_EPOCH="$(date +%s)"
SHUTDOWN_DURATION=$((SHUTDOWN_END_EPOCH-SHUTDOWN_START_EPOCH))
{
  echo "shutdown_seconds=${SHUTDOWN_DURATION}"
} >> "${TIME_LOG}"


APP_DB="${HOME_DIR}/data/application.db"
BREAKDOWN_LOG="${LOG_DIR}/disk-breakdown.log"
if [ -d "${APP_DB}" ]; then
  {
    echo "app_db=${APP_DB}"
    echo "du_human:"
    du -sh "${APP_DB}" "${APP_DB}"/* 2>/dev/null || true
    echo "du_bytes:"
    if du -sb "${APP_DB}" >/dev/null 2>&1; then
      du -sb "${APP_DB}" "${APP_DB}"/* 2>/dev/null || true
    else
      du -sk "${APP_DB}" "${APP_DB}"/* 2>/dev/null | awk '{print $1 * 1024 " " $2}'
    fi
    echo "top_files_bytes:"
    print_top_files_by_size "${APP_DB}" 20
  } > "${BREAKDOWN_LOG}"
  log_info "Disk breakdown log: ${BREAKDOWN_LOG}"
fi

if [ -n "${TREEDB_TRACE_PATH}" ]; then
  write_treedb_trace_report "${TREEDB_TRACE_ANALYSIS_PATH}" "post-run" || true
  if [ -s "${TREEDB_TRACE_ANALYSIS_PATH}" ]; then
    {
      echo "treedb_trace_analysis_path=${TREEDB_TRACE_ANALYSIS_PATH}"
    } >> "${TIME_LOG}"
    log_info "TreeDB trace analysis: ${TREEDB_TRACE_ANALYSIS_PATH}"
  fi
fi

trap - EXIT
log_info "Run complete. Time log: ${TIME_LOG}"
