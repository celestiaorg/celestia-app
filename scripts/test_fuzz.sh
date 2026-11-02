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
	# Compare "package target" pairs: a target registered under the wrong
	# package fuzzes nothing, and go test still exits 0.
	registered=$(targets | awk '{print $2" "$3}' | sort)
	defined=$(git grep --untracked -E '^func Fuzz[A-Za-z0-9_]*\(' -- '*_test.go' |
		sed -E 's/^([^:]*):func (Fuzz[A-Za-z0-9_]*)\(.*/\1 \2/' |
		awk '{n = split($1, p, "/"); dir = "."; for (i = 1; i < n; i++) dir = dir "/" p[i]; print dir " " $2}' | sort)
	status=0
	if dups=$(printf '%s\n' "$registered" | uniq -d) && [ -n "$dups" ]; then
		echo "Registered more than once in $registry:"; echo "$dups"; status=1
	fi
	if missing=$(comm -13 <(printf '%s\n' "$registered" | uniq) <(printf '%s\n' "$defined" | uniq)) && [ -n "$missing" ]; then
		echo "Fuzz targets missing from $registry:"; echo "$missing"; status=1
	fi
	if stale=$(comm -23 <(printf '%s\n' "$registered" | uniq) <(printf '%s\n' "$defined" | uniq)) && [ -n "$stale" ]; then
		echo "Registered targets with no matching func in that package:"; echo "$stale"; status=1
	fi
	exit $status
fi

shard=${FUZZ_SHARD:-0}
shards=${FUZZ_SHARDS:-1}
if [ -n "${FUZZ_LOG_DIR:-}" ]; then
	mkdir -p "$FUZZ_LOG_DIR"
	: >"$FUZZ_LOG_DIR/failed.txt"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

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
	log=${FUZZ_LOG_DIR:-$tmp}/$target.log
	# go test exits 0 with "no fuzz tests to fuzz" when the target is missing.
	if go test "${args[@]}" "$pkg" >"$log" 2>&1 && ! grep -q 'no fuzz tests to fuzz' "$log"; then
		ok=1
	else
		ok=0
	fi
	if [ -z "${FUZZ_LOG_DIR:-}" ]; then
		cat "$log"
	fi
	if [ "$ok" = 1 ]; then
		echo "PASS $target"
	elif [ -n "${FUZZ_LOG_DIR:-}" ]; then
		echo "FAIL $target (output withheld; see the Linear issue)"
		echo "$priority $pkg $target" >>"$FUZZ_LOG_DIR/failed.txt"
		failed=1
	else
		echo "FAIL $target"
		failed=1
	fi
done < <(targets)

exit $failed
