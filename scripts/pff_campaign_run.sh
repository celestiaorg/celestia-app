#!/usr/bin/env bash
# Run the frozen 5,800-PFF ProcessProposal workload and report cold plus warm.
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cache_dir=/root/Documents/Codex/.go-cache
export GOPATH="$cache_dir/gopath"
export GOMODCACHE="$cache_dir/pkg/mod"
export GOCACHE="$cache_dir/build"
export CELESTIA_APP_HOME="$cache_dir/.celestia-app"
mkdir -p "$GOPATH" "$GOMODCACHE" "$GOCACHE" "$CELESTIA_APP_HOME"
cd "$repo_dir"

taskset -c 0-15 go test -tags=benchmarks -run='^$' \
  -bench='^BenchmarkProcessProposal_PFF$' -benchtime=1x -count=1 \
  -timeout=180m ./app/benchmarks/ | awk '
  { print }
  $1 ~ /^BenchmarkProcessProposal_PFF\/pff=5800\/cache=cold-/ {
    for (i = 2; i < NF; i++) if ($(i + 1) == "ns/op") cold = $i / 1000000
  }
  $1 ~ /^BenchmarkProcessProposal_PFF\/pff=5800\/cache=warm-/ {
    for (i = 2; i < NF; i++) if ($(i + 1) == "ns/op") warm = $i / 1000000
  }
  END {
    if (cold == "" || warm == "") exit 1
    printf "PFF_COLD_MS: %.3f\nPFF_WARM_MS: %.3f\nPFF_TOTAL_MS: %.3f\n", cold, warm, cold + warm
  }
'
