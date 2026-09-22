#!/usr/bin/env bash
# Logs NIC bytes, CPU, memory, and fibre RSS once per second to /root/samples.csv.
# Usage: nohup ./sample.sh >/dev/null 2>&1 &
set -u
iface=$(ip route show default | awk '{print $5; exit}')
echo "ts,rx_bytes,tx_bytes,cpu_busy_pct,mem_used_kib,fibre_rss_kib" > /root/samples.csv
read -r _ u n s i w q sq st _ < /proc/stat
prev_busy=$((u + n + s + q + sq + st)); prev_total=$((prev_busy + i + w))
while true; do
  sleep 1
  read -r _ u n s i w q sq st _ < /proc/stat
  busy=$((u + n + s + q + sq + st)); total=$((busy + i + w))
  cpu=$(( (busy - prev_busy) * 100 / (total - prev_total + 1) ))
  prev_busy=$busy; prev_total=$total
  rx=$(cat /sys/class/net/"$iface"/statistics/rx_bytes)
  tx=$(cat /sys/class/net/"$iface"/statistics/tx_bytes)
  mem=$(awk '/MemTotal/{t=$2} /MemAvailable/{a=$2} END{print t-a}' /proc/meminfo)
  rss=$(ps -C fibre,fibre-txsim -o rss= | awk '{s+=$1} END{print s+0}')
  echo "$(date +%s),$rx,$tx,$cpu,$mem,$rss" >> /root/samples.csv
done
