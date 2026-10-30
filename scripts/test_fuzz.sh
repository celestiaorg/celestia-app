#!/usr/bin/env bash
# Runs the fuzz targets registered in scripts/fuzz_targets.txt.
#
# The Go toolchain only fuzzes one target in one package per invocation, so each
# target is registered explicitly. When you add a fuzz target, add it to
# scripts/fuzz_targets.txt; --check fails on unregistered or stale entries.
# See docs/fuzzing.md for the risk-ranked entry-point inventory and limitations.
#
# Usage:
#   scripts/test_fuzz.sh          run every target
#   scripts/test_fuzz.sh --check  verify the registry matches the Fuzz* functions
#
# Environment:
#   FUZZ_SHARD, FUZZ_SHARDS  run only targets whose registry index i satisfies
#                            i % FUZZ_SHARDS == FUZZ_SHARD (default: all).
#   FUZZ_LOG_DIR             write each target's output to FUZZ_LOG_DIR instead
#                            of stdout, and list failing targets in
#                            FUZZ_LOG_DIR/failed.txt. CI uses this to keep
#                            crash inputs out of public logs.
set -euo pipefail

cd "$(dirname "$0")/.."
registry=scripts/fuzz_targets.txt

targets() {
	grep -Ev '^[[:space:]]*(#|$)' "$registry"
}

if [ "${1:-}" = "--check" ]; then
	registered=$(targets | awk '{print $3}' | sort)
	defined=$(git grep --untracked -h -E '^func Fuzz[A-Za-z0-9_]*\(' -- '*_test.go' | sed -E 's/^func (Fuzz[A-Za-z0-9_]*)\(.*/\1/' | sort)
	status=0
	if dups=$(printf '%s\n' "$registered" | uniq -d) && [ -n "$dups" ]; then
		echo "Registered more than once in $registry:"; echo "$dups"; status=1
	fi
	if missing=$(comm -13 <(printf '%s\n' "$registered" | uniq) <(printf '%s\n' "$defined" | uniq)) && [ -n "$missing" ]; then
		echo "Fuzz targets missing from $registry:"; echo "$missing"; status=1
	fi
	if stale=$(comm -23 <(printf '%s\n' "$registered" | uniq) <(printf '%s\n' "$defined" | uniq)) && [ -n "$stale" ]; then
		echo "Registered targets with no matching func in the repository:"; echo "$stale"; status=1
	fi
	exit $status
fi

shard=${FUZZ_SHARD:-0}
shards=${FUZZ_SHARDS:-1}
if [ -n "${FUZZ_LOG_DIR:-}" ]; then
	mkdir -p "$FUZZ_LOG_DIR"
	: >"$FUZZ_LOG_DIR/failed.txt"
fi

failed=0
index=0
while read -r priority pkg target fuzztime minimizetime; do
	if [ $((index % shards)) -ne "$shard" ]; then
		index=$((index + 1))
		continue
	fi
	index=$((index + 1))

	args=(-run='^$' -fuzz="^${target}\$" -fuzztime "$fuzztime")
	if [ "$minimizetime" != "-" ]; then
		args+=(-fuzzminimizetime="$minimizetime")
	fi

	echo "==> $target ($pkg, priority $priority)"
	if [ -n "${FUZZ_LOG_DIR:-}" ]; then
		if go test "${args[@]}" "$pkg" >"$FUZZ_LOG_DIR/$target.log" 2>&1; then
			echo "PASS $target"
		else
			echo "FAIL $target (output withheld; see the Linear issue)"
			echo "$priority $pkg $target" >>"$FUZZ_LOG_DIR/failed.txt"
			failed=1
		fi
	elif ! go test "${args[@]}" "$pkg"; then
		echo "FAIL $target"
		failed=1
	fi
done < <(targets)

exit $failed
