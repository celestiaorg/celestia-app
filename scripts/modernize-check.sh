#!/bin/bash

# Modernize check script for celestia-app CI
# This script checks for modernize issues without applying fixes
# Fails if any modernize issues are found

set -e

cd "$(dirname "$0")/.."

echo "Running Go modernize check..."

# Find all Go packages and exclude those containing generated .pb.go files
PACKAGES=$(go list ./... | while read -r pkg; do
    pkg_dir=$(go list -f '{{.Dir}}' "$pkg")
    if ! ls "$pkg_dir"/*.pb.go >/dev/null 2>&1; then
        echo "$pkg"
    fi
done)

if [ -z "$PACKAGES" ]; then
    echo "No packages to check"
    exit 0
fi

# Run modernize without -fix flag to only check for issues
# The tool will exit with non-zero code if issues are found
echo "$PACKAGES" | xargs go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@v0.21.0 -test
