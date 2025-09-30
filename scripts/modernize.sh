#!/bin/bash

# Modernize script for celestia-app
# Follow-up to issue #5709 and PR #5852

set -e

cd "$(dirname "$0")/.."

echo "Running Go modernize tool..."
go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...

echo "Modernize fixes applied successfully!"
