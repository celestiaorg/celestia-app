#!/usr/bin/env bash
set -euo pipefail

export HOSTNAME=$(hostname)
PROMTAIL_CONFIG=/root/promtail-config.yml
printf "%s" "__PROMTAIL_CONFIG_B64__" | base64 -d > "$PROMTAIL_CONFIG"

if ! command -v promtail >/dev/null 2>&1; then
  arch=$(uname -m)
  if [ "$arch" = "x86_64" ] || [ "$arch" = "amd64" ]; then arch=amd64;
  elif [ "$arch" = "aarch64" ] || [ "$arch" = "arm64" ]; then arch=arm64;
  else echo "unsupported arch: $arch" >&2; exit 1; fi
  apt-get update -y >/dev/null
  apt-get install -y curl unzip >/dev/null
  tmpdir=$(mktemp -d)
  curl -fsSL -o "$tmpdir/promtail.zip" "https://github.com/grafana/loki/releases/download/v2.9.3/promtail-linux-$arch.zip"
  unzip -o "$tmpdir/promtail.zip" -d "$tmpdir" >/dev/null
  install -m 0755 "$tmpdir/promtail-linux-$arch" /usr/local/bin/promtail
fi

promtail -config.file="$PROMTAIL_CONFIG" -config.expand-env -server.http-listen-port=9080 > /root/promtail.log 2>&1 &
sleep 1
pgrep -a promtail >/dev/null 2>&1 || (echo "promtail failed to start:" >&2; tail -200 /root/promtail.log >&2; exit 1)

__LATENCY_MONITOR_CMD__
