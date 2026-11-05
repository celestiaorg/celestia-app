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
#   FUZZ_DROP_FAILED_CORPUS  if 1, delete a failing target's generated corpus
#                            under $(go env GOCACHE)/fuzz, so CI never caches
#                            inputs near a crash where other jobs can read them.
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

# only_fuzztime_expired reports whether the sole failure in a go test log is
# the fuzz coordinator reporting its own -fuzztime deadline. When -fuzztime
# expires, the coordinator can read the expired context before the workers'
# child context is cancelled, so it fails to recognise the deadline as its own
# and prints "--- FAIL: <target>" followed only by "context deadline exceeded",
# without writing a failing input. That is a clean finish, not a crash: a real
# crash always writes a failing input or prints more than the bare error.
only_fuzztime_expired() {
	local log=$1 target=$2
	! grep -q 'Failing input written' "$log" || return 1
	awk -v target="$target" '
		$0 ~ "^--- FAIL: " target " \\(" { failing = 1; fails++; next }
		failing && /^(FAIL|exit status|ok)([[:space:]]|$)/ { failing = 0 }
		failing && NF {
			if ($0 ~ /^[[:space:]]+context deadline exceeded$/) { deadline++ } else { other++ }
		}
		END { exit !(fails == 1 && deadline == 1 && other == 0) }
	' "$log"
}

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
	elif only_fuzztime_expired "$log" "$target"; then
		echo "NOTE $target: go test reported its own -fuzztime deadline as a failure; no failing input was found"
		ok=1
	else
		ok=0
	fi
	if [ -z "${FUZZ_LOG_DIR:-}" ]; then
		cat "$log"
	fi
	if [ "$ok" = 0 ] && [ "${FUZZ_DROP_FAILED_CORPUS:-}" = 1 ]; then
		if import_path=$(go list "$pkg") && [ -n "$import_path" ]; then
			rm -rf "$(go env GOCACHE)/fuzz/$import_path/$target"
		else
			# Without the import path the corpus cannot be located, so drop
			# every generated corpus rather than risk caching a near-crash.
			rm -rf "$(go env GOCACHE)/fuzz"
		fi
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
