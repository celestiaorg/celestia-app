#!/usr/bin/env bash
# Run the frozen 5,800-PFF ProcessProposal workload and report cold plus warm.
#
# PFF_BENCH_CORES pins the run to a fixed core set so results are comparable
# across runs; the parallel verifier sizes its fan-out from runtime.NumCPU(),
# which GOMAXPROCS does not change. PFF_BENCH_CACHE_DIR redirects the Go caches
# when the default location is on a small or shared volume.
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_dir"

if [ -n "${PFF_BENCH_CACHE_DIR:-}" ]; then
  export GOPATH="$PFF_BENCH_CACHE_DIR/gopath"
  export GOMODCACHE="$PFF_BENCH_CACHE_DIR/pkg/mod"
  export GOCACHE="$PFF_BENCH_CACHE_DIR/build"
  export CELESTIA_APP_HOME="$PFF_BENCH_CACHE_DIR/.celestia-app"
  mkdir -p "$GOPATH" "$GOMODCACHE" "$GOCACHE" "$CELESTIA_APP_HOME"
fi

cores=${PFF_BENCH_CORES:-0-15}
cmd=(go test -tags=benchmarks -run='^$'
  -bench='^BenchmarkProcessProposal_PFF$'
  "-benchtime=${PFF_BENCH_TIME:-5x}" "-count=${PFF_BENCH_COUNT:-10}"
  -timeout=180m ./app/benchmarks/)
if command -v taskset >/dev/null 2>&1; then
  cmd=(taskset -c "$cores" "${cmd[@]}")
else
  echo "taskset not found; running unpinned, results are not comparable across machines" >&2
fi

"${cmd[@]}" | awk '
  { print }
  # Every sample is kept and the median reported: a single run of this
  # workload has been observed to swing by 2x, enough to invert cold and warm.
  $1 ~ /\/cache=cold-/ {
    for (i = 2; i < NF; i++) if ($(i + 1) == "ns/op") cold[++nc] = $i / 1000000
  }
  $1 ~ /\/cache=warm-/ {
    for (i = 2; i < NF; i++) if ($(i + 1) == "ns/op") warm[++nw] = $i / 1000000
  }
  function median(a, n,   i, j, t) {
    for (i = 1; i <= n; i++) for (j = i + 1; j <= n; j++) if (a[j] < a[i]) { t = a[i]; a[i] = a[j]; a[j] = t }
    return (n % 2) ? a[(n + 1) / 2] : (a[n / 2] + a[n / 2 + 1]) / 2
  }
  END {
    if (nc == 0 || nw == 0) {
      print "no cold/warm samples parsed: did the benchmark name or workload change?" > "/dev/stderr"
      exit 1
    }
    c = median(cold, nc); w = median(warm, nw)
    printf "PFF_SAMPLES: %d cold, %d warm\n", nc, nw
    printf "PFF_COLD_MS: %.3f\nPFF_WARM_MS: %.3f\nPFF_TOTAL_MS: %.3f\n", c, w, c + w
    printf "PFF_COLD_RANGE_MS: %.3f-%.3f\n", cold[1], cold[nc]
    printf "PFF_WARM_RANGE_MS: %.3f-%.3f\n", warm[1], warm[nw]
  }
'
