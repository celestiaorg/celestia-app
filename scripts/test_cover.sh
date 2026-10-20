#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
MODULE=$(GOWORK=off go list -m)

# CI splits the suite into shards that run on separate runners. Each named
# shard holds the slowest test packages; "rest" runs every other package, so a
# new package is always covered. Keep the matrix in test-coverage.yml in sync.
declare -A SHARDS=(
    [app-test]="app/test"
    [user]="pkg/user"
    [txsim]="test/txsim test/ledger test/cmd/txsim"
)
if [[ "${1:-}" == "--list-shards" ]]; then
    printf '%s\n' "${!SHARDS[@]}" rest | LC_ALL=C sort
    exit 0
fi

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
OUT_OF_SCOPE="^$MODULE/(test|tools|scripts|local_devnet|pkg/rsema1d/cmd/testvectors)(/|$)"
SCOPE_PKGS=$(printf '%s\n' "$PKGS" | grep -Ev "$OUT_OF_SCOPE")

# Only eligible packages are instrumented, so test utilities, tools and scripts
# never enter the profile. Tests in every package in PKGS still run and credit
# the eligible code they exercise.
COVER_PKGS=$(printf '%s\n' "$SCOPE_PKGS" | paste -sd, -)

# Select the packages whose tests run. Instrumentation always spans the full
# eligible scope, so every shard reports the same blocks.
SHARD=${COVER_SHARD:-}
if [[ -n "$SHARD" ]]; then
    SHARDED=$(printf '%s\n' "${SHARDS[@]}" | tr ' ' '\n' | sed "s|^|$MODULE/|")
    if [[ "$SHARD" == "rest" ]]; then
        PKGS=$(printf '%s\n' "$PKGS" | grep -vxF "$SHARDED" || true)
    elif [[ -n "${SHARDS[$SHARD]+set}" ]]; then
        # A renamed or deleted package fails go test instead of shrinking the shard.
        PKGS=$(printf '%s\n' "${SHARDS[$SHARD]}" | tr ' ' '\n' | sed "s|^|$MODULE/|")
    else
        echo "unknown coverage shard: $SHARD" >&2
        exit 1
    fi
    if [[ -z "$PKGS" ]]; then
        echo "coverage shard $SHARD selects no packages" >&2
        exit 1
    fi
fi

# Keep the previous report intact if tests fail. Create the temporary profile
# beside the destination so publishing it is an atomic rename.
COVER_DIR=$(mktemp -d ./coverage.XXXXXX)
trap 'rm -rf "$COVER_DIR"' EXIT

# -p 1 below also serializes compilation, so build the instrumented test
# binaries in parallel first. -run '^$' executes no tests. Build errors are
# reported by the real run.
# shellcheck disable=SC2086
go test -run '^$' -covermode=atomic -coverpkg="$COVER_PKGS" $PKGS > /dev/null 2>&1 || true

# -coverpkg credits coverage to the package that owns the code rather than the
# package whose tests exercised it, so tests in app/test count toward app/.
# -p 1 runs packages serially because the testnode-based suites collide on
# ports and time out when run concurrently.
# shellcheck disable=SC2086
go test -p 1 -timeout 60m -coverprofile="$COVER_DIR/coverage.txt" -covermode=atomic -coverpkg="$COVER_PKGS" $PKGS

bash "$SCRIPT_DIR/merge_coverage.sh" coverage.txt "$COVER_DIR/coverage.txt"
