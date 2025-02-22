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

echo "mode: atomic" > coverage.txt
for pkg in "${PKGS[@]}"; do
    go test -v -timeout 30m -test.short -coverprofile=profile.out -covermode=atomic "$pkg"
    if [ -f profile.out ]; then
        tail -n +2 profile.out >> coverage.txt;
        rm profile.out
    fi
done
