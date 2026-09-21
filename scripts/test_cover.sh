#!/usr/bin/env bash
set -e

# Define the directories to exclude
EXCLUDE_DIRS=("/test/util")

# Initialize PKGS variable with the list of all packages
PKGS=$(go list ./...)

# Loop over the directories to exclude and remove them from PKGS
for DIR in "${EXCLUDE_DIRS[@]}"; do
    PKGS=$(echo "$PKGS" | grep -v "$DIR")
done

# -coverpkg credits coverage to the package that owns the code rather than the
# package whose tests exercised it, so tests in app/test count toward app/.
# -p 1 runs packages serially because the testnode-based suites collide on
# ports and time out when run concurrently.
# shellcheck disable=SC2086
go test -p 1 -timeout 60m -coverprofile=coverage.txt -covermode=atomic -coverpkg=./... $PKGS
