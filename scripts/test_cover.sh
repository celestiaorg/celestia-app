#!/usr/bin/env bash
set -euo pipefail

# Define the directories to exclude
EXCLUDE_DIRS=("/test/util")

# Initialize PKGS variable with the list of all packages
PKGS=$(go list ./...)

# Loop over the directories to exclude and remove them from PKGS
for DIR in "${EXCLUDE_DIRS[@]}"; do
    PKGS=$(echo "$PKGS" | grep -v "$DIR")
done

# Use the same scope for instrumentation and test selection. In particular,
# imported test utilities must not re-enter the profile through -coverpkg.
COVER_PKGS=$(printf '%s\n' "$PKGS" | paste -sd, -)

# Keep the previous report intact if tests fail. Create the temporary profile
# beside the destination so publishing it is an atomic rename.
COVER_DIR=$(mktemp -d ./coverage.XXXXXX)
trap 'rm -rf "$COVER_DIR"' EXIT

# -coverpkg credits coverage to the package that owns the code rather than the
# package whose tests exercised it, so tests in app/test count toward app/.
# -p 1 runs packages serially because the testnode-based suites collide on
# ports and time out when run concurrently.
# shellcheck disable=SC2086
go test -p 1 -timeout 60m -coverprofile="$COVER_DIR/coverage.txt" -covermode=atomic -coverpkg="$COVER_PKGS" $PKGS

# Go can emit the same block from several test binaries, including zero-count
# entries for packages without tests. Count each block once in the denominator.
{
    echo 'mode: atomic'
    awk 'NR > 1 {
        statements[$1] = $2
        counts[$1] += $3
    }
    END {
        for (block in statements)
            printf "%s %d %.0f\n", block, statements[block], counts[block]
    }' "$COVER_DIR/coverage.txt" | LC_ALL=C sort
} > "$COVER_DIR/merged.txt"
mv "$COVER_DIR/merged.txt" coverage.txt
