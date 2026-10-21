#!/usr/bin/env bash
set -euo pipefail

# Usage: merge_coverage.sh OUTPUT PROFILE...
#
# Go can emit the same block from several test binaries, including zero-count
# entries for packages without tests. Count each block once in the denominator
# and sum its executions. Shard profiles merge the same way, so merging the
# outputs of several runs equals one run over all of their packages. Generated
# *.pb.go and *.pb.gw.go files are dropped.
if [[ $# -lt 2 ]]; then
    echo "usage: $0 OUTPUT PROFILE..." >&2
    exit 2
fi
OUTPUT=$1
shift
for profile in "$@"; do
    if [[ "$(head -n 1 "$profile")" != "mode: atomic" ]]; then
        echo "$profile is not an atomic coverage profile" >&2
        exit 1
    fi
done

# Write beside the destination so publishing it is an atomic rename.
TMP=$(mktemp "$OUTPUT.XXXXXX")
trap 'rm -f "$TMP"' EXIT
{
    echo 'mode: atomic'
    awk 'FNR > 1 && $1 !~ /\.pb(\.gw)?\.go:/ {
        statements[$1] = $2
        counts[$1] += $3
    }
    END {
        for (block in statements)
            printf "%s %d %.0f\n", block, statements[block], counts[block]
    }' "$@" | LC_ALL=C sort
} > "$TMP"
mv "$TMP" "$OUTPUT"
