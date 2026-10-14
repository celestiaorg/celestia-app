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

# Eligible coverage scope: production packages only. Keep this in sync with the
# ignore list in .github/codecov.yml. Generated *.pb.go and *.pb.gw.go files are
# dropped from the merged profile below.
OUT_OF_SCOPE="^$(GOWORK=off go list -m)/(test|tools|scripts|local_devnet|pkg/rsema1d/cmd/testvectors)(/|$)"
SCOPE_PKGS=$(printf '%s\n' "$PKGS" | grep -Ev "$OUT_OF_SCOPE")

# Only eligible packages are instrumented, so test utilities, tools and scripts
# never enter the profile. Tests in every package in PKGS still run and credit
# the eligible code they exercise.
COVER_PKGS=$(printf '%s\n' "$SCOPE_PKGS" | paste -sd, -)

# Keep the previous report intact if tests fail. Create the temporary profile
# beside the destination so publishing it is an atomic rename.
COVER_DIR=$(mktemp -d ./coverage.XXXXXX)
trap 'rm -rf "$COVER_DIR"' EXIT

# Go can emit the same block from several test binaries, including zero-count
# entries for packages without tests. Count each block once in the denominator.
merge() {
    echo 'mode: atomic'
    awk 'NR > 1 && $1 !~ /\.pb(\.gw)?\.go:/ {
        statements[$1] = $2
        counts[$1] += $3
    }
    END {
        for (block in statements)
            printf "%s %d %.0f\n", block, statements[block], counts[block]
    }' "$1" | LC_ALL=C sort
}

# -coverpkg credits coverage to the package that owns the code rather than the
# package whose tests exercised it, so tests in app/test count toward app/.
# -p 1 runs packages serially because the testnode-based suites collide on
# ports and time out when run concurrently.
#
# The -short run skips the in-process network (testnode) tests and gives unit
# coverage in coverage-unit.txt. The full run adds them and gives coverage.txt.
# shellcheck disable=SC2086
go test -short -p 1 -timeout 60m -coverprofile="$COVER_DIR/unit.txt" -covermode=atomic -coverpkg="$COVER_PKGS" $PKGS
# shellcheck disable=SC2086
go test -p 1 -timeout 60m -coverprofile="$COVER_DIR/coverage.txt" -covermode=atomic -coverpkg="$COVER_PKGS" $PKGS

merge "$COVER_DIR/unit.txt" > "$COVER_DIR/unit-merged.txt"
merge "$COVER_DIR/coverage.txt" > "$COVER_DIR/merged.txt"
mv "$COVER_DIR/unit-merged.txt" coverage-unit.txt
mv "$COVER_DIR/merged.txt" coverage.txt
