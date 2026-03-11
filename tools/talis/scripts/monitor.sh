#!/usr/bin/env bash
# monitor.sh — per-port network bandwidth + per-process CPU/memory monitoring.
# Writes JSONL to /root/monitor.jsonl (one sample per INTERVAL seconds).
# Designed to run on Linux validators with iptables and /proc.

set -euo pipefail

INTERVAL="${MONITOR_INTERVAL:-1}"
OUTPUT="/root/monitor.jsonl"
PORTS="9091 26656 26657"
PROCESS_NAMES="celestia-appd fibre-txsim txsim"

# ---------- iptables accounting setup ----------

setup_iptables() {
  iptables -N MONITOR_IN  2>/dev/null || true
  iptables -N MONITOR_OUT 2>/dev/null || true

  # Remove old jump rules (ignore errors if absent)
  iptables -D INPUT  -j MONITOR_IN  2>/dev/null || true
  iptables -D OUTPUT -j MONITOR_OUT 2>/dev/null || true

  # Flush any previous per-port rules
  iptables -F MONITOR_IN
  iptables -F MONITOR_OUT

  # Insert jump rules at the top of INPUT/OUTPUT
  iptables -I INPUT  1 -j MONITOR_IN
  iptables -I OUTPUT 1 -j MONITOR_OUT

  # Add per-port accounting rules
  for port in $PORTS; do
    iptables -A MONITOR_IN  -p tcp --dport "$port"
    iptables -A MONITOR_OUT -p tcp --sport "$port"
  done
}

cleanup_iptables() {
  iptables -D INPUT  -j MONITOR_IN  2>/dev/null || true
  iptables -D OUTPUT -j MONITOR_OUT 2>/dev/null || true
  iptables -F MONITOR_IN  2>/dev/null || true
  iptables -F MONITOR_OUT 2>/dev/null || true
  iptables -X MONITOR_IN  2>/dev/null || true
  iptables -X MONITOR_OUT 2>/dev/null || true
}

trap cleanup_iptables EXIT
setup_iptables

# ---------- helpers ----------

# read_iptables_bytes <chain>
# Outputs one line per rule: "<port> <bytes>"
read_iptables_bytes() {
  local chain="$1"
  iptables -L "$chain" -v -n -x 2>/dev/null | awk '
    /tcp/ {
      # Find the port: look for dpt: or spt: field
      for (i = 1; i <= NF; i++) {
        if ($i ~ /^[ds]pt:/) {
          split($i, a, ":")
          print a[2], $2   # port, bytes
        }
      }
    }
  '
}

# get_proc_stat <pid> — outputs "utime stime num_threads" from /proc/<pid>/stat
get_proc_stat() {
  local pid="$1"
  # Fields: pid (comm) state ... (field 14=utime, 15=stime, 20=num_threads)
  awk '{print $14, $15, $20}' "/proc/$pid/stat" 2>/dev/null || echo "0 0 0"
}

# get_proc_rss_mb <pid> — outputs VmRSS in MB from /proc/<pid>/status
get_proc_rss_mb() {
  local pid="$1"
  awk '/^VmRSS:/ {printf "%.1f", $2/1024}' "/proc/$pid/status" 2>/dev/null || echo "0"
}

# get_total_cpu_ticks — sum of all fields from first line of /proc/stat
get_total_cpu_ticks() {
  awk '/^cpu / {sum=0; for(i=2;i<=NF;i++) sum+=$i; print sum}' /proc/stat
}

# get_system_mem — outputs "used_mb total_mb"
get_system_mem() {
  awk '
    /^MemTotal:/ { total=$2 }
    /^MemAvailable:/ { avail=$2 }
    END { printf "%.0f %.0f", (total-avail)/1024, total/1024 }
  ' /proc/meminfo
}

# ---------- initial snapshot ----------

declare -A prev_in_bytes
declare -A prev_out_bytes
declare -A prev_proc_ticks

# Seed network counters
while IFS=' ' read -r port bytes; do
  prev_in_bytes["$port"]="$bytes"
done < <(read_iptables_bytes MONITOR_IN)

while IFS=' ' read -r port bytes; do
  prev_out_bytes["$port"]="$bytes"
done < <(read_iptables_bytes MONITOR_OUT)

# Seed CPU counters
prev_total_ticks=$(get_total_cpu_ticks)
for name in $PROCESS_NAMES; do
  pid=$(pgrep -x "$name" 2>/dev/null | head -1 || true)
  if [ -n "$pid" ]; then
    read -r ut st _threads <<< "$(get_proc_stat "$pid")"
    prev_proc_ticks["$name"]=$((ut + st))
  else
    prev_proc_ticks["$name"]=0
  fi
done

sleep "$INTERVAL"

# ---------- main loop ----------

while true; do
  ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

  # --- network deltas ---
  net_json="{"
  first=true
  while IFS=' ' read -r port bytes; do
    prev=${prev_in_bytes["$port"]:-0}
    delta=$(( (bytes - prev) / INTERVAL ))
    prev_in_bytes["$port"]="$bytes"
    if [ "$first" = true ]; then first=false; else net_json+=","; fi
    net_json+="\"in_${port}_bytes_sec\":${delta}"
  done < <(read_iptables_bytes MONITOR_IN)

  while IFS=' ' read -r port bytes; do
    prev=${prev_out_bytes["$port"]:-0}
    delta=$(( (bytes - prev) / INTERVAL ))
    prev_out_bytes["$port"]="$bytes"
    net_json+=",\"out_${port}_bytes_sec\":${delta}"
  done < <(read_iptables_bytes MONITOR_OUT)
  net_json+="}"

  # --- per-process CPU + memory ---
  cur_total_ticks=$(get_total_cpu_ticks)
  total_delta=$((cur_total_ticks - prev_total_ticks))
  prev_total_ticks=$cur_total_ticks

  proc_json="{"
  first=true
  for name in $PROCESS_NAMES; do
    pid=$(pgrep -x "$name" 2>/dev/null | head -1 || true)
    if [ -n "$pid" ]; then
      read -r ut st threads <<< "$(get_proc_stat "$pid")"
      cur_ticks=$((ut + st))
      prev_t=${prev_proc_ticks["$name"]:-0}
      if [ "$total_delta" -gt 0 ]; then
        # cpu_pct with one decimal: (proc_delta * 10000 / total_delta) then insert decimal
        raw=$(( (cur_ticks - prev_t) * 10000 / total_delta ))
        cpu_pct="$((raw / 10)).$((raw % 10))"
      else
        cpu_pct="0.0"
      fi
      prev_proc_ticks["$name"]=$cur_ticks
      rss_mb=$(get_proc_rss_mb "$pid")
    else
      cpu_pct="0.0"
      rss_mb="0"
      threads="0"
      prev_proc_ticks["$name"]=0
    fi

    if [ "$first" = true ]; then first=false; else proc_json+=","; fi
    proc_json+="\"${name}\":{\"cpu_pct\":${cpu_pct},\"rss_mb\":${rss_mb},\"threads\":${threads}}"
  done
  proc_json+="}"

  # --- system-wide stats ---
  if [ "$total_delta" -gt 0 ]; then
    # System idle ticks are field 5 of /proc/stat cpu line
    idle_ticks=$(awk '/^cpu / {print $5}' /proc/stat)
    # We need current and previous idle, but for simplicity compute from total usage.
    # Instead, use load average which is readily available.
    :
  fi
  read -r mem_used mem_total <<< "$(get_system_mem)"
  load_1m=$(awk '{print $1}' /proc/loadavg)
  # System CPU%: from /proc/stat, compute as (1 - idle_delta/total_delta) * 100
  idle_now=$(awk '/^cpu / {print $5}' /proc/stat)
  # We need the previous idle, store it
  if [ -z "${prev_idle:-}" ]; then
    prev_idle=$idle_now
  fi
  idle_delta=$((idle_now - prev_idle))
  prev_idle=$idle_now
  if [ "$total_delta" -gt 0 ]; then
    busy_delta=$((total_delta - idle_delta))
    raw=$(( busy_delta * 1000 / total_delta ))
    sys_cpu="$((raw / 10)).$((raw % 10))"
  else
    sys_cpu="0.0"
  fi

  sys_json="{\"cpu_pct\":${sys_cpu},\"load_1m\":${load_1m},\"mem_used_mb\":${mem_used},\"mem_total_mb\":${mem_total}}"

  # --- emit JSONL line ---
  echo "{\"ts\":\"${ts}\",\"net\":${net_json},\"proc\":${proc_json},\"sys\":${sys_json}}" >> "$OUTPUT"

  sleep "$INTERVAL"
done
