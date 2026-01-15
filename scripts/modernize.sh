#!/bin/bash

# Modernize script for celestia-app
# Follow-up to issue #5709 and PR #5852

set -e

cd "$(dirname "$0")/.."

echo "Running Go modernize tool..."

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

echo "$PACKAGES" | xargs go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@v0.21.0 -fix -test

echo "Modernize fixes applied successfully!"
